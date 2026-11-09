#ifndef MM_PORTABLE_H
#define MM_PORTABLE_H
#include <stdint.h>
typedef struct mm_audio mm_audio;
mm_audio *mm_audio_open(const char *url, int network);
void mm_audio_close(mm_audio *);
void mm_audio_play(mm_audio *, int);
void mm_audio_seek(mm_audio *, double);
void mm_audio_volume(mm_audio *, float);
int mm_audio_state(mm_audio *);
double mm_audio_position(mm_audio *);
double mm_audio_duration(mm_audio *);
uint64_t mm_audio_consumed(mm_audio *);
uint64_t mm_audio_underruns(mm_audio *);
double mm_audio_energy(mm_audio *);
double mm_audio_output_energy(mm_audio *);
#endif
