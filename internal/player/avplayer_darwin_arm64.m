//go:build darwin && arm64 && cgo

#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>
#include <math.h>
#include <stdlib.h>
#include <string.h>

@interface MMPlayback : NSObject {
@public
    AVPlayer *player;
    id endObserver;
    id failObserver;
    BOOL ended;
    BOOL failed;
    BOOL seeking;
    BOOL audioOnly;
    NSUInteger seekGeneration;
    double seekPosition;
}
@end

@implementation MMPlayback
- (void)dealloc {
    [player release];
    [super dealloc];
}
@end

void *mm_av_open(const char *url) {
    @autoreleasepool {
        NSURL *u = [NSURL URLWithString:[NSString stringWithUTF8String:url]];
        if (!u) return NULL;
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:u options:nil];
        AVPlayerItem *item = [AVPlayerItem playerItemWithAsset:asset];
        MMPlayback *p = [[MMPlayback alloc] init];
        p->player = [[AVPlayer alloc] initWithPlayerItem:item];
        p->player.actionAtItemEnd = AVPlayerActionAtItemEndPause;
        p->endObserver = [[[NSNotificationCenter defaultCenter] addObserverForName:AVPlayerItemDidPlayToEndTimeNotification object:item queue:nil usingBlock:^(NSNotification *n) {
            @synchronized(p) { p->ended = YES; }
        }] retain];
        p->failObserver = [[[NSNotificationCenter defaultCenter] addObserverForName:AVPlayerItemFailedToPlayToEndTimeNotification object:item queue:nil usingBlock:^(NSNotification *n) {
            @synchronized(p) { p->failed = YES; }
        }] retain];
        return p;
    }
}

void mm_av_close(void *handle) {
    @autoreleasepool {
        MMPlayback *p = handle;
        [p->player pause];
        [p->player.currentItem cancelPendingSeeks];
        [[NSNotificationCenter defaultCenter] removeObserver:p->endObserver];
        [[NSNotificationCenter defaultCenter] removeObserver:p->failObserver];
        [p->endObserver release];
        [p->failObserver release];
        [p->player replaceCurrentItemWithPlayerItem:nil];
        [p release];
    }
}

void mm_av_play(void *handle, int play) {
    @autoreleasepool {
        MMPlayback *p = handle;
        if (play) [p->player play]; else [p->player pause];
    }
}

void mm_av_seek(void *handle, double seconds) {
    @autoreleasepool {
        MMPlayback *p = handle;
        NSUInteger generation;
        @synchronized(p) {
            p->ended = NO;
            p->seeking = YES;
            p->seekPosition = seconds;
            generation = ++p->seekGeneration;
        }
        [p->player seekToTime:CMTimeMakeWithSeconds(seconds, 1000000) toleranceBefore:kCMTimeZero toleranceAfter:kCMTimeZero completionHandler:^(BOOL finished) {
            @synchronized(p) {
                if (generation == p->seekGeneration) p->seeking = NO;
            }
        }];
    }
}

void mm_av_volume(void *handle, float volume) {
    @autoreleasepool { ((MMPlayback *)handle)->player.volume = volume; }
}

static double seconds(CMTime t) {
    double s = CMTimeGetSeconds(t);
    return isfinite(s) && s > 0 ? s : 0;
}

double mm_av_position(void *handle) {
    @autoreleasepool {
        MMPlayback *p = handle;
        @synchronized(p) { if (p->seeking) return p->seekPosition; }
        return seconds(p->player.currentTime);
    }
}

double mm_av_duration(void *handle) {
    @autoreleasepool { return seconds(((MMPlayback *)handle)->player.currentItem.duration); }
}

int mm_av_state(void *handle) {
    @autoreleasepool {
        MMPlayback *p = handle;
        @synchronized(p) {
            if (p->failed || p->player.status == AVPlayerStatusFailed || p->player.currentItem.status == AVPlayerItemStatusFailed) return -1;
            if (p->ended) { p->ended = NO; return 2; }
        }
        if (p->player.currentItem.status != AVPlayerItemStatusReadyToPlay) return 0;
        if (!p->audioOnly) {
            for (AVPlayerItemTrack *track in p->player.currentItem.tracks) {
                if (![track.assetTrack.mediaType isEqualToString:AVMediaTypeAudio]) track.enabled = NO;
            }
            p->audioOnly = YES;
        }
        return 1;
    }
}

char *mm_av_error(void *handle) {
    @autoreleasepool {
        MMPlayback *p = handle;
        NSError *e = p->player.currentItem.error ?: p->player.error;
        return strdup((e.localizedDescription ?: @"Audio playback failed").UTF8String);
    }
}
