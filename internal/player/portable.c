//go:build darwin && arm64 && cgo

#define _DARWIN_C_SOURCE 1
#include "portable.h"
#include <math.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#define MINIAUDIO_IMPLEMENTATION
#define MA_NO_DECODING
#define MA_NO_ENCODING
#define MA_NO_RESOURCE_MANAGER
#define MA_NO_NODE_GRAPH
#define MA_NO_ENGINE
#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/channel_layout.h>
#include <libswresample/swresample.h>
#include <miniaudio.h>

struct mm_audio {
  pthread_t thread;
  pthread_mutex_t mutex;
  pthread_cond_t wake;
  char *url;
  int network, primary_opened;
  atomic_int stop, playing, state, eof, intentional_stop, device_failed;
  atomic_uint serial;
  atomic_uint active;
  double requested, base, origin;
  _Atomic double duration, position, energy, output_energy;
  _Atomic float volume;
  atomic_ullong consumed, underruns;
  AVFormatContext *format;
  AVCodecContext *codec;
  SwrContext *swr;
  AVPacket *packet;
  AVFrame *frame;
  int stream, packet_pending, draining, decoder_done;
  ma_pcm_rb ring;
  ma_device device;
  int ring_ready, device_ready, device_started;
  float *samples;
  unsigned sample_capacity;
};

static pthread_once_t log_once = PTHREAD_ONCE_INIT;
static void quiet_logs(void) { av_log_set_level(AV_LOG_QUIET); }

static int interrupted(void *opaque) {
  mm_audio *a = opaque;
  return atomic_load(&a->stop) || (a->format && atomic_load(&a->state) != 0 &&
                                   atomic_load(&a->serial) != a->active);
}
static int nested_open(AVFormatContext *s, AVIOContext **io, const char *url,
                       int flags, AVDictionary **options) {
  mm_audio *a = s->opaque;
  if (!a || a->primary_opened || strcmp(url, a->url) != 0)
    return AVERROR(EACCES);
  a->primary_opened = 1;
  return avio_open2(io, url, flags, &s->interrupt_callback, options);
}
static void callback(ma_device *device, void *output, const void *input,
                     ma_uint32 frames) {
  (void)input;
  mm_audio *a = device->pUserData;
  float *dst = output;
  memset(dst, 0, (size_t)frames * 2 * sizeof(float));
  if (!atomic_load(&a->playing) || atomic_load(&a->stop) ||
      atomic_load(&a->serial) != atomic_load(&a->active))
    return;
  ma_uint32 left = frames;
  float volume = atomic_load(&a->volume);
  double energy = 0, output_energy = 0;
  while (left) {
    ma_uint32 n = left;
    void *src = NULL;
    ma_pcm_rb_acquire_read(&a->ring, &n, &src);
    if (!n)
      break;
    float *p = src;
    for (ma_uint32 i = 0; i < n * 2; i++) {
      energy += (double)p[i] * p[i];
      dst[i] = p[i] * volume;
      output_energy += (double)dst[i] * dst[i];
    }
    ma_pcm_rb_commit_read(&a->ring, n);
    dst += n * 2;
    left -= n;
  }
  unsigned count = frames - left;
  atomic_fetch_add(&a->consumed, count);
  atomic_store(&a->position,
               a->base + (double)atomic_load(&a->consumed) / 48000.0);
  atomic_store(&a->energy, atomic_load(&a->energy) + energy);
  atomic_store(&a->output_energy,
               atomic_load(&a->output_energy) + output_energy);
  if (left && !atomic_load(&a->eof))
    atomic_fetch_add(&a->underruns, 1);
  if (atomic_load(&a->eof) && ma_pcm_rb_available_read(&a->ring) == 0)
    atomic_store(&a->state, 2);
  pthread_cond_signal(&a->wake);
}
static void notification(const ma_device_notification *event) {
  mm_audio *a = event->pDevice->pUserData;
  if (event->type == ma_device_notification_type_stopped &&
      !atomic_load(&a->intentional_stop) && !atomic_load(&a->stop) &&
      atomic_load(&a->playing) && atomic_load(&a->state) != 2) {
    atomic_store(&a->device_failed, 1);
    atomic_store(&a->state, -1);
    pthread_cond_signal(&a->wake);
  }
}
static void stop_device(mm_audio *a);
static void wait_work(mm_audio *a) {
  pthread_mutex_lock(&a->mutex);
  if ((!atomic_load(&a->playing) || atomic_load(&a->state) == 2) &&
      a->device_started) {
    pthread_mutex_unlock(&a->mutex);
    stop_device(a);
    pthread_mutex_lock(&a->mutex);
  }
  if (!atomic_load(&a->stop) && atomic_load(&a->serial) == a->active) {
    struct timespec until;
    clock_gettime(CLOCK_REALTIME, &until);
    until.tv_nsec += 20000000;
    if (until.tv_nsec >= 1000000000) {
      until.tv_sec++;
      until.tv_nsec -= 1000000000;
    }
    if (atomic_load(&a->playing) && atomic_load(&a->state) != 2)
      pthread_cond_timedwait(&a->wake, &a->mutex, &until);
    else
      pthread_cond_wait(&a->wake, &a->mutex);
  }
  pthread_mutex_unlock(&a->mutex);
}
static void stop_device(mm_audio *a) {
  if (a->device_started) {
    atomic_store(&a->intentional_stop, 1);
    ma_device_stop(&a->device);
    a->device_started = 0;
    atomic_store(&a->intentional_stop, 0);
  }
}
static int open_input(mm_audio *a) {
  a->format = avformat_alloc_context();
  if (!a->format)
    return -1;
  a->format->interrupt_callback = (AVIOInterruptCB){interrupted, a};
  a->format->opaque = a;
  a->format->io_open = nested_open;
  AVDictionary *opts = NULL;
  av_dict_set(&opts, "protocol_whitelist", a->network ? "http,tcp" : "file", 0);
  av_dict_set(&opts, "format_whitelist",
              "mov,mp3,flac,wav,matroska,webm,ogg,aac,wv,aiff,ape,asf", 0);
  av_dict_set(&opts, "rw_timeout", "30000000", 0);
  av_dict_set(&opts, "multiple_requests", "1", 0);
  av_dict_set(&opts, "http_proxy", "", 0);
  av_dict_set(&opts, "request_size", "1048576", 0);
  av_dict_set(&opts, "short_seek_size", "32768", 0);
  av_dict_set(&opts, "use_mfra_for", "0", 0);
  int result = avformat_open_input(&a->format, a->url, NULL, &opts);
  av_dict_free(&opts);
  if (result < 0 || avformat_find_stream_info(a->format, NULL) < 0)
    return -1;
  a->stream =
      av_find_best_stream(a->format, AVMEDIA_TYPE_AUDIO, -1, -1, NULL, 0);
  if (a->stream < 0)
    return -1;
  AVStream *stream = a->format->streams[a->stream];
  const AVCodec *codec = avcodec_find_decoder(stream->codecpar->codec_id);
  if (!codec)
    return -1;
  a->codec = avcodec_alloc_context3(codec);
  if (!a->codec ||
      avcodec_parameters_to_context(a->codec, stream->codecpar) < 0 ||
      avcodec_open2(a->codec, codec, NULL) < 0)
    return -1;
  AVChannelLayout stereo = AV_CHANNEL_LAYOUT_STEREO;
  if (swr_alloc_set_opts2(&a->swr, &stereo, AV_SAMPLE_FMT_FLT, 48000,
                          &a->codec->ch_layout, a->codec->sample_fmt,
                          a->codec->sample_rate, 0, NULL) < 0 ||
      swr_init(a->swr) < 0)
    return -1;
  a->origin = stream->start_time == AV_NOPTS_VALUE
                  ? 0
                  : stream->start_time * av_q2d(stream->time_base);
  double duration = stream->duration == AV_NOPTS_VALUE
                        ? 0
                        : stream->duration * av_q2d(stream->time_base);
  if (duration <= 0 && a->format->duration != AV_NOPTS_VALUE)
    duration = (double)a->format->duration / AV_TIME_BASE;
  atomic_store(&a->duration, fmax(0, duration));
  a->packet = av_packet_alloc();
  a->frame = av_frame_alloc();
  if (!a->packet || !a->frame)
    return -1;
  if (ma_pcm_rb_init(ma_format_f32, 2, 96000, NULL, NULL, &a->ring) !=
      MA_SUCCESS)
    return -1;
  a->ring_ready = 1;
  ma_device_config cfg = ma_device_config_init(ma_device_type_playback);
  cfg.playback.format = ma_format_f32;
  cfg.playback.channels = 2;
  cfg.sampleRate = 48000;
  cfg.periodSizeInMilliseconds = 20;
  cfg.dataCallback = callback;
  cfg.notificationCallback = notification;
  cfg.pUserData = a;
  if (ma_device_init(NULL, &cfg, &a->device) != MA_SUCCESS)
    return -1;
  a->device_ready = 1;
  return 0;
}
static int apply_seek(mm_audio *a) {
  unsigned serial;
  double target;
  pthread_mutex_lock(&a->mutex);
  serial = atomic_load(&a->serial);
  target = a->requested;
  pthread_mutex_unlock(&a->mutex);
  if (serial == a->active)
    return 0;
  stop_device(a);
  atomic_store(&a->state, 1);
  a->active = serial;
  int64_t timestamp =
      av_rescale_q((int64_t)llround((target + a->origin) * AV_TIME_BASE),
                   AV_TIME_BASE_Q, a->format->streams[a->stream]->time_base);
  int result =
      av_seek_frame(a->format, a->stream, timestamp, AVSEEK_FLAG_BACKWARD);
  if (result < 0) {
    if (atomic_load(&a->serial) != a->active)
      return 1;
    return -1;
  }
  avcodec_flush_buffers(a->codec);
  swr_close(a->swr);
  if (swr_init(a->swr) < 0)
    return -1;
  av_packet_unref(a->packet);
  av_frame_unref(a->frame);
  a->packet_pending = a->draining = a->decoder_done = 0;
  ma_pcm_rb_reset(&a->ring);
  a->base = target;
  atomic_store(&a->consumed, 0);
  atomic_store(&a->position, target);
  atomic_store(&a->eof, 0);
  atomic_store(&a->state, 1);
  return 1;
}
static int put_samples(mm_audio *a, float *samples, int frames) {
  int offset = 0;
  while (offset < frames) {
    if (atomic_load(&a->stop) || atomic_load(&a->serial) != a->active)
      return 1;
    ma_uint32 n = frames - offset;
    void *dst = NULL;
    ma_pcm_rb_acquire_write(&a->ring, &n, &dst);
    if (n) {
      memcpy(dst, samples + offset * 2, (size_t)n * 2 * sizeof(float));
      ma_pcm_rb_commit_write(&a->ring, n);
      offset += n;
    } else {
      if (!atomic_load(&a->playing))
        stop_device(a);
      else if (!a->device_started) {
        if (ma_device_start(&a->device) != MA_SUCCESS)
          return -1;
        a->device_started = 1;
      }
      wait_work(a);
    }
  }
  return 0;
}
static int convert(mm_audio *a, AVFrame *frame) {
  int capacity = swr_get_out_samples(a->swr, frame ? frame->nb_samples : 0);
  if (capacity < 0)
    return -1;
  if (capacity < 1)
    capacity = 1;
  if ((unsigned)capacity > a->sample_capacity) {
    float *samples =
        av_realloc_array(a->samples, (size_t)capacity * 2, sizeof(float));
    if (!samples)
      return -1;
    a->samples = samples;
    a->sample_capacity = capacity;
  }
  double pts = frame && frame->best_effort_timestamp != AV_NOPTS_VALUE
                   ? frame->best_effort_timestamp *
                             av_q2d(a->format->streams[a->stream]->time_base) -
                         a->origin -
                         (double)swr_get_delay(a->swr, a->codec->sample_rate) /
                             a->codec->sample_rate
                   : a->base;
  uint8_t *out = (uint8_t *)a->samples;
  int n = swr_convert(a->swr, &out, capacity,
                      frame ? (const uint8_t **)frame->extended_data : NULL,
                      frame ? frame->nb_samples : 0);
  if (n < 0)
    return -1;
  int skip =
      frame && pts < a->base ? (int)fmin(n, ceil((a->base - pts) * 48000)) : 0;
  if (put_samples(a, a->samples + skip * 2, n - skip) < 0)
    return -1;
  return n;
}
static int decode_step(mm_audio *a) {
  int result = avcodec_receive_frame(a->codec, a->frame);
  if (result >= 0) {
    result = convert(a, a->frame);
    av_frame_unref(a->frame);
    return result < 0 ? -1 : 0;
  }
  if (result == AVERROR_EOF) {
    int tail;
    do {
      tail = convert(a, NULL);
      if (tail < 0)
        return -1;
      if (interrupted(a))
        return 0;
    } while (tail > 0);
    a->decoder_done = 1;
    atomic_store(&a->eof, 1);
    return 0;
  }
  if (result != AVERROR(EAGAIN))
    return -1;
  if (a->draining)
    return -1;
  if (!a->packet_pending) {
    do {
      result = av_read_frame(a->format, a->packet);
      if (result < 0)
        break;
      if (a->packet->stream_index == a->stream) {
        a->packet_pending = 1;
        break;
      }
      av_packet_unref(a->packet);
    } while (!interrupted(a));
    if (result < 0) {
      if (interrupted(a))
        return 0;
      if (result != AVERROR_EOF)
        return -1;
      result = avcodec_send_packet(a->codec, NULL);
      if (result == 0 || result == AVERROR_EOF)
        a->draining = 1;
      return result < 0 && result != AVERROR(EAGAIN) && result != AVERROR_EOF
                 ? -1
                 : 0;
    }
  }
  if (interrupted(a))
    return 0;
  result = avcodec_send_packet(a->codec, a->packet);
  if (result != AVERROR(EAGAIN)) {
    av_packet_unref(a->packet);
    a->packet_pending = 0;
  }
  return result < 0 && result != AVERROR(EAGAIN) ? -1 : 0;
}
static void *worker(void *opaque) {
  mm_audio *a = opaque;
  if (open_input(a) < 0) {
    if (!atomic_load(&a->stop))
      atomic_store(&a->state, -1);
    return NULL;
  }
  atomic_store(&a->state, 1);
  while (!atomic_load(&a->stop)) {
    if (atomic_load(&a->state) < 0 || atomic_load(&a->device_failed))
      break;
    if (apply_seek(a) < 0) {
      atomic_store(&a->state, -1);
      break;
    }
    if (atomic_load(&a->serial) != a->active)
      continue;
    int playing = atomic_load(&a->playing);
    if (!playing || atomic_load(&a->state) == 2)
      stop_device(a);
    else if (!a->device_started &&
             (ma_pcm_rb_available_read(&a->ring) >= 4800 || a->decoder_done)) {
      if (ma_device_start(&a->device) != MA_SUCCESS) {
        atomic_store(&a->state, -1);
        break;
      }
      a->device_started = 1;
    }
    if (a->decoder_done) {
      if (ma_pcm_rb_available_read(&a->ring) == 0) {
        atomic_store(&a->state, 2);
        stop_device(a);
      }
      wait_work(a);
      continue;
    }
    if (ma_pcm_rb_available_write(&a->ring) < 8192) {
      wait_work(a);
      continue;
    }
    if (decode_step(a) < 0) {
      atomic_store(&a->state, -1);
      break;
    }
  }
  stop_device(a);
  return NULL;
}
mm_audio *mm_audio_open(const char *url, int network) {
  pthread_once(&log_once, quiet_logs);
  mm_audio *a = calloc(1, sizeof(*a));
  if (!a)
    return NULL;
  a->url = strdup(url);
  a->network = network;
  atomic_init(&a->volume, 1);
  pthread_mutex_init(&a->mutex, NULL);
  pthread_cond_init(&a->wake, NULL);
  if (!a->url || pthread_create(&a->thread, NULL, worker, a) != 0) {
    free(a->url);
    pthread_cond_destroy(&a->wake);
    pthread_mutex_destroy(&a->mutex);
    free(a);
    return NULL;
  }
  return a;
}
void mm_audio_close(mm_audio *a) {
  if (!a)
    return;
  atomic_store(&a->stop, 1);
  atomic_store(&a->playing, 0);
  pthread_mutex_lock(&a->mutex);
  pthread_cond_broadcast(&a->wake);
  pthread_mutex_unlock(&a->mutex);
  pthread_join(a->thread, NULL);
  if (a->device_ready)
    ma_device_uninit(&a->device);
  if (a->ring_ready)
    ma_pcm_rb_uninit(&a->ring);
  av_free(a->samples);
  av_packet_free(&a->packet);
  av_frame_free(&a->frame);
  swr_free(&a->swr);
  avcodec_free_context(&a->codec);
  avformat_close_input(&a->format);
  pthread_cond_destroy(&a->wake);
  pthread_mutex_destroy(&a->mutex);
  free(a->url);
  free(a);
}
void mm_audio_play(mm_audio *a, int playing) {
  if (!a)
    return;
  atomic_store(&a->playing, playing);
  pthread_mutex_lock(&a->mutex);
  pthread_cond_signal(&a->wake);
  pthread_mutex_unlock(&a->mutex);
}
void mm_audio_seek(mm_audio *a, double seconds) {
  if (!a)
    return;
  pthread_mutex_lock(&a->mutex);
  a->requested = fmax(0, seconds);
  atomic_fetch_add(&a->serial, 1);
  pthread_cond_signal(&a->wake);
  pthread_mutex_unlock(&a->mutex);
}
void mm_audio_volume(mm_audio *a, float volume) {
  if (a)
    atomic_store(&a->volume, fminf(1, fmaxf(0, volume)));
}
int mm_audio_state(mm_audio *a) {
  if (!a || atomic_load(&a->device_failed))
    return -1;
  int pending = atomic_load(&a->serial) != atomic_load(&a->active);
  int state = atomic_load(&a->state);
  if (state == 2 && pending)
    return 1;
  return state;
}
double mm_audio_position(mm_audio *a) {
  return a ? atomic_load(&a->position) : 0;
}
double mm_audio_duration(mm_audio *a) {
  return a ? atomic_load(&a->duration) : 0;
}
uint64_t mm_audio_consumed(mm_audio *a) {
  return a ? atomic_load(&a->consumed) : 0;
}
uint64_t mm_audio_underruns(mm_audio *a) {
  return a ? atomic_load(&a->underruns) : 0;
}
double mm_audio_energy(mm_audio *a) { return a ? atomic_load(&a->energy) : 0; }

double mm_audio_output_energy(mm_audio *a) {
  return a ? atomic_load(&a->output_energy) : 0;
}
