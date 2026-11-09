/*
 * ffdecode.c — FFmpeg H264/AAC decoder wrapper DLL (static-linked).
 *
 * Exposes flat C functions for H264/AAC decoding via FFmpeg's libavcodec.
 * FFmpeg is statically linked into this DLL, so no external FFmpeg DLLs needed.
 * Hardware acceleration (D3D11VA/DXVA2 on Windows, VideoToolbox on macOS)
 * is enabled by default and falls back to software if unavailable.
 *
 * Build (Windows, MinGW):
 *   gcc -std=c11 -shared -O2 -static -o ffdecode.dll ffdecode.c \
 *     -I<ffmpeg_include> -L<ffmpeg_lib> \
 *     -lavcodec -lavutil -lswscale -lswresample \
 *     -ld3d11 -ldxva2 -lws2_32 -lbcrypt -lSecur32 -lOle32 -lUser32 -lShlwapi -lAdvapi32 -lShell32 -lm \
 *     -Wl,--whole-archive -lwinpthread -Wl,--no-whole-archive
 *
 * Build (macOS, Clang):
 *   clang -shared -O2 -o ffdecode.dylib ffdecode.c \
 *     -I<ffmpeg_include> -L<ffmpeg_lib> \
 *     -lavcodec -lavutil -lswscale -lswresample \
 *     -framework CoreFoundation -framework CoreMedia -framework VideoToolbox
 */

#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdio.h>
#include <time.h>

#ifdef _WIN32
#include <windows.h>
#include <objbase.h>
#include <d3d11.h>
#endif


#ifdef __cplusplus
extern "C" {
#endif
#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
#include <libavutil/opt.h>
#include <libavutil/samplefmt.h>
#include <libavutil/hwcontext.h>
#ifdef _WIN32
#include <libavutil/hwcontext_d3d11va.h>
#endif
#include <libswscale/swscale.h>

#include <libswresample/swresample.h>
#ifdef __cplusplus
}
#endif

/* ─── Exported API ─────────────────────────────────────────────────────────── */

/* C++ 编译时保持 C 符号名（避免 name mangling 导致 Go purego 找不到导出函数） */
#if defined(_WIN32) && defined(__cplusplus)
#define FFDEC_API extern "C" __declspec(dllexport)
#elif defined(_WIN32)
#define FFDEC_API __declspec(dllexport)
#else
#define FFDEC_API extern "C" __attribute__((visibility("default")))
#endif

FFDEC_API void FF_Init(void);
FFDEC_API void* FF_CreateDecoder(void);
FFDEC_API void FF_DestroyDecoder(void* handle);
FFDEC_API int  FF_Decode(void* handle, const uint8_t* data, int dataLen,
                         uint8_t** outBGRA, int* outWidth, int* outHeight);
FFDEC_API int  FF_Flush(void* handle, uint8_t** outBGRA, int* outWidth, int* outHeight);
FFDEC_API void FF_SetHWAccel(void* handle, int mode);
FFDEC_API const char* FF_GetDecoderInfo(void* handle);

/* ─── D3D11 Texture Sharing API (GPU rendering path) ──────────────────────── */

FFDEC_API int  FF_GetD3D11Device(void* handle, void** outDevice);
FFDEC_API void FF_SetD3D11Device(void* handle, void* device);
FFDEC_API int FF_NeedPLI(void* handle);
FFDEC_API int  FF_DecodeTexture(void* handle, const uint8_t* data, int dataLen,
                                void** outTexture, int* outSubIdx, int* outWidth, int* outHeight,
                                void** outFrame);
/* FF_ReceiveBGRA: FF_DecodeTexture 返回 4（软解帧已 receive 或纹理输出被禁用）后调用，
   对已 receive 的 d->frame 调 frameToBGRA 输出 BGRA，避免 Go 侧重复 send_packet 导致马赛克。 */
FFDEC_API int  FF_ReceiveBGRA(void* handle, uint8_t** outBGRA, int* outWidth, int* outHeight);

/* FF_SetTextureOutput 开关纹理输出路径。默认 1（硬解返回 NV12 纹理）。
   0 时硬解帧也走 FF_ReceiveBGRA（transfer_data + sws_scale 输出 BGRA），
   供渲染器无 NV12 SRV 能力（无 D3D11.3 PlaneSlice）时回退，避免 GPU 模式黑屏。 */
FFDEC_API void FF_SetTextureOutput(void* handle, int enable);

/* FF_ReopenSoftware：硬解卡死时回退软解（mpv force_fallback 等价物）。
   avcodec_close + 清 hwDevCtx + avcodec_open2(无 hw) → 纯 CPU 软解。
   必须在解码线程调（D3D11VA 资源同线程销毁，COM 同线程）。
   返回 0=成功切软解，<0=失败。 */
FFDEC_API int FF_ReopenSoftware(void* handle);

/* FF_IsSoftware：返回 1=当前 codec 已开为软解（无 hwDevCtx 或 codec 已关），
   0=硬解。供 Go 侧判断是否已回退，避免重复调用。 */
FFDEC_API int FF_IsSoftware(void* handle);


/* ─── AAC Decoder API ─────────────────────────────────────────────────────── */

FFDEC_API void* FF_CreateAACDecoder(int sampleRate, int channels);
FFDEC_API int  FF_DecodeAAC(void* handle, const uint8_t* data, int dataLen,
                             uint8_t* outBuf, int outBufSize);
FFDEC_API void FF_DestroyAACDecoder(void* handle);

/* ─── Internal ─────────────────────────────────────────────────────────────── */

/* 黑帧滑动窗口参数（占比判定，与 Go 侧 blackframe_window.go 同策略，改动需两处同步）：
   BLACK_WIN_MS 窗口宽度、阈值 25%（黑覆盖 ≥75ms 判异常丢弃保帧）、
   BLACK_MAX_SEGS 窗口内最多保留的黑段数（60fps 交替约 10 段，2 倍冗余）、
   BLACK_EST_CAP_MS 孤立黑帧时长估算封顶（50ms < 25% 阈值 75ms → 稀疏单帧黑
   不会被误拦；连续黑从第二帧起走精确累计）。 */
#define BLACK_WIN_MS     300
#define BLACK_MAX_SEGS   24
#define BLACK_EST_CAP_MS 50

typedef struct { uint64_t start, end; } FFBlackSeg;  /* 连续黑帧段 [start,end) ms */

typedef struct FFDecoder {
    AVCodecContext*        ctx;
    const AVCodec*         codec;
    AVFrame*               frame;
    AVFrame*               swFrame;
    struct SwsContext*     sws;
    AVPacket*              pkt;

    int                    needpli;       /* 绿帧/黑帧标记需补帧（Go 侧查询后发 PLI） */
    int                    badStreak;     /* 连续 UV=0 坏帧计数（单帧丢弃保上一帧，≥2 帧降级灰度） */
    int                    frameErrorFlags; /* 最近 receive 帧的 decode_error_flags（FF_GetFrameErrorFlags 读） */
    /* 黑帧滑动窗口（占比判定）：窗口内黑帧按连续性合并成段，最近 BLACK_WIN_MS
       内黑段覆盖时长之和 ≥25% 判异常 → 丢弃保上一帧；<25% 放行（宽容：
       低频黑可能是真黑屏内容）。不依赖 streak 连续性，成对交替也能拦住。 */
    FFBlackSeg             blackSegs[BLACK_MAX_SEGS];
    int                    blackSegN;         /* blackSegs 有效段数 */
    int                    blackLastWasBlack; /* 上一帧是否黑（用于段延长/合并） */
    uint64_t               blackLastMs;       /* 上一帧到达时刻，0=无（估算当前黑帧时长用） */
    unsigned               blackDropped;      /* 累计丢弃的黑帧数（诊断日志用） */
    unsigned               blackShown;        /* 累计放行显示的黑帧数（诊断日志用） */

    int                    textureOutput; /* 1=硬解返回 NV12 纹理（默认），0=硬解也转 BGRA 输出 */

    uint8_t*               bgraBuf;
    int                    bgraBufSize;

    int                    hwAccel;
    int                    width;
    int                    height;
    enum AVPixelFormat     swsSrcFmt;

    AVBufferRef*           hwDevCtx;

    void*                  d3d11Device;   /* cached from hwDevCtx */
    void*                  extDevice;     /* 外部传入的 D3D11 device（渲染器 device），优先用于创建 hwdevice */


} FFDecoder;

static int ffInitDone = 0;

/* Forward declarations (needed because FF_DecodeTexture calls openCodec/isHwPixelFormat,
   and FF_DestroyDecoder calls comRelease, before their definitions) */
#ifdef _WIN32
static ULONG comAddRef(void* obj);
static ULONG comRelease(void* obj);
#endif
static int openCodec(FFDecoder* d);
static int isHwPixelFormat(enum AVPixelFormat fmt);
static int frameToBGRA(FFDecoder* d, AVFrame* frame, uint8_t** outBGRA, int* outWidth, int* outHeight);

FFDEC_API void FF_Init(void) {
    ffInitDone = 1;
}

FFDEC_API void* FF_CreateDecoder(void) {
    FFDecoder* d = (FFDecoder*)calloc(1, sizeof(FFDecoder));
    if (!d) return NULL;

    FF_Init();

    d->codec = avcodec_find_decoder(AV_CODEC_ID_H264);
    if (!d->codec) {
        free(d);
        return NULL;
    }

    d->ctx = avcodec_alloc_context3(d->codec);
    if (!d->ctx) {
        free(d);
        return NULL;
    }

    d->ctx->flags |= AV_CODEC_FLAG_LOW_DELAY;
    d->ctx->flags2 |= AV_CODEC_FLAG2_FAST;
    av_opt_set(d->ctx->priv_data, "threads", "1", 0);

    d->frame = av_frame_alloc();
    d->swFrame = av_frame_alloc();
    d->pkt = av_packet_alloc();

    d->hwAccel = 1; /* D3D11VA/DXVA2 auto-detect, fallback to software */
    d->textureOutput = 1; /* 默认纹理输出（零拷贝），渲染器无 NV12 SRV 能力时由 Go 侧关闭 */

    /* Do NOT call openCodec here — D3D11VA resources must be created and
       destroyed on the same thread. openCodec will be called lazily on the
       first FF_Decode invocation (which runs on the UI thread via the
       SDL pump timer), ensuring thread-safe D3D11 lifecycle. */

    return d;
}

FFDEC_API void FF_DestroyDecoder(void* handle) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d) return;

#ifdef _WIN32
    if (d->d3d11Device) {
        comRelease(d->d3d11Device);
        d->d3d11Device = NULL;
    }
    if (d->extDevice) {
        comRelease(d->extDevice);
        d->extDevice = NULL;
    }
#endif

    if (d->sws) sws_freeContext(d->sws);
    if (d->frame) av_frame_free(&d->frame);
    if (d->swFrame) av_frame_free(&d->swFrame);
    if (d->pkt) av_packet_free(&d->pkt);
    /* hwDevCtx aliases ctx->hw_device_ctx (same AVBufferRef). avcodec_free_context
       → avcodec_close unrefs hw_device_ctx; detach it first so the single ref is
       unref'd exactly once below (otherwise double-free → 0xc0000374 heap crash
       on hardware-decode teardown). */
    if (d->ctx) {
        d->ctx->hw_device_ctx = NULL;
        avcodec_free_context(&d->ctx);
    }
    if (d->hwDevCtx) av_buffer_unref(&d->hwDevCtx);
    free(d->bgraBuf);
    free(d);
}

FFDEC_API void FF_SetHWAccel(void* handle, int mode) {
    FFDecoder* d = (FFDecoder*)handle;
    if (d) d->hwAccel = mode;
}

static char decoderInfoBuf[256];

FFDEC_API const char* FF_GetDecoderInfo(void* handle) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d) return "no decoder";

    const char* hwType = "none";
    if (d->hwDevCtx && d->ctx && d->ctx->hw_device_ctx && d->ctx->hw_device_ctx->data) {
        AVHWDeviceContext* devCtx = (AVHWDeviceContext*)d->ctx->hw_device_ctx->data;
        hwType = av_hwdevice_get_type_name(devCtx->type);
        if (!hwType) hwType = "unknown";
    }

    snprintf(decoderInfoBuf, sizeof(decoderInfoBuf),
             "v2 hwAccel=%d hwType=%s codecOpen=%d",
             d->hwAccel, hwType, avcodec_is_open(d->ctx));
    return decoderInfoBuf;
}

/* ─── D3D11 Same-Device Rendering Implementation ──────────────────────────── */

#ifdef _WIN32

/* Extract ID3D11Device* from FFmpeg's AVBufferRef hw device context.
   MinGW's d3d11.h uses C-style vtable, so we work through void* and vtable indices. */
static void* getD3D11DeviceFromCtx(FFDecoder* d) {
    if (!d->hwDevCtx || !d->hwDevCtx->data) return NULL;
    AVHWDeviceContext* hwDevCtx = (AVHWDeviceContext*)d->hwDevCtx->data;
    if (hwDevCtx->type != AV_HWDEVICE_TYPE_D3D11VA) return NULL;
    void* hwctx = hwDevCtx->hwctx;
    if (!hwctx) return NULL;
    return *(void**)hwctx;  /* ID3D11Device* */
}

/* Extract ID3D11DeviceContext* from FFmpeg's hw device context.
   AVD3D11VADeviceContext 第二个字段是 device_context。 */
static void* getD3D11DeviceContextFromCtx(FFDecoder* d) {
    if (!d->hwDevCtx || !d->hwDevCtx->data) return NULL;
    AVHWDeviceContext* hwDevCtx = (AVHWDeviceContext*)d->hwDevCtx->data;
    if (hwDevCtx->type != AV_HWDEVICE_TYPE_D3D11VA) return NULL;
    void* hwctx = hwDevCtx->hwctx;
    if (!hwctx) return NULL;
    return *((void**)hwctx + 1);  /* ID3D11DeviceContext* */
}

/* COM vtable indices — verified against Windows SDK d3d11.h */
#define VTBL_IDX_AddRef                1
#define VTBL_IDX_Release               2

static ULONG comAddRef(void* obj) {
    if (!obj) return 0;
    void** vtbl = *(void***)obj;
    typedef ULONG (__stdcall *Fn)(void*);
    return ((Fn)vtbl[VTBL_IDX_AddRef])(obj);
}

static ULONG comRelease(void* obj) {
    if (!obj) return 0;
    void** vtbl = *(void***)obj;
    typedef ULONG (__stdcall *Fn)(void*);
    return ((Fn)vtbl[VTBL_IDX_Release])(obj);
}

#endif /* _WIN32 */

/* ─── D3D11 API Exports ────────────────────────────────────────────────────── */

FFDEC_API int FF_GetD3D11Device(void* handle, void** outDevice) {
#ifndef _WIN32
    return -1;
#else
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !outDevice) return -1;

    void* dev = d->d3d11Device;
    if (!dev) {
        dev = getD3D11DeviceFromCtx(d);
        if (!dev) return -1;
        comAddRef(dev);
        d->d3d11Device = dev;
    }

    *outDevice = dev;
    comAddRef(dev);
    return 0;
#endif
}

/* FF_SetD3D11Device 设外部 D3D11 device（渲染器 device），Initialize/openCodec 时用它
   创建 hwdevice_ctx（av_hwdevice_ctx_alloc + 手动设 hwctx->device），让解码与渲染同设备，
   根治跨设备 CopySubresourceRegion 问题（mpv/VLC 标准方案）。须在 Initialize 前调用。 */
FFDEC_API void FF_SetD3D11Device(void* handle, void* device) {
#ifndef _WIN32
    (void)handle; (void)device;
#else
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !device) return;
    if (d->extDevice) comRelease(d->extDevice);
    d->extDevice = device;
    comAddRef(device);
#endif
}

/* FF_ResetKeyFrame 已删除：关键帧门控回退旧版行为（不丢弃非 IDR 帧，收到什么渲染什么）。
   FFmpeg 自身会拒绝无参考帧的 P 帧（receive_frame 报错），无需额外门控。 */

FFDEC_API void FF_SetTextureOutput(void* handle, int enable) {
    FFDecoder* d = (FFDecoder*)handle;
    if (d) d->textureOutput = (enable != 0);
}

/* FF_GetFrameErrorFlags 返回解码器最近一次 receive 出的帧的 decode_error_flags
   （FF_DECODE_ERROR_* 位掩码：1=INVALID_BITSTREAM 2=MISSING_REFERENCE
   4=CONCEALMENT_ACTIVE 8=DECODE_SLICES）。智能丢帧丢 P/B 后解码器参考链断裂，
   FFmpeg 会用错误掩盖机制输出「缺参考/掩盖」帧（内容是旧画面+撕裂/灰块混合），
   画面随机异常 → 探针难判（不是全黑、UV 非全 0），上屏即闪。解码器自己知道
   错了：由 Go 侧在渲染前查询本标志，非 0 则丢弃保上一帧。
   可选导出（旧版 dll 缺失时 Go 侧自动跳过）。 */
FFDEC_API int FF_GetFrameErrorFlags(void* handle) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !d->ctx) return -1;
    return d->frameErrorFlags;
}

/* FF_ReopenSoftware：硬解卡死回退软解（mpv force_fallback 等价物）。
   FFmpeg 8.1 无 avcodec_close，用 avcodec_free_context 销毁 ctx（连带释放
   hw_device_ctx/hwframe pool），再 alloc 新 ctx + 重设 flags + avcodec_open2
   （无 hw_device_ctx → 纯 CPU 软解）。必须在解码线程调（与 openCodec 同线程，
   D3D11VA 资源同线程销毁）。返回 0=成功切软解，<0=失败。 */
FFDEC_API int FF_ReopenSoftware(void* handle) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !d->ctx) return -1;

    /* hwDevCtx aliases ctx->hw_device_ctx（同 AVBufferRef）。free_context 内部
       unref hw_device_ctx，先 detach 避免双 free（与 DestroyDecoder 同款处理）。 */
    d->ctx->hw_device_ctx = NULL;
    avcodec_free_context(&d->ctx);
    if (d->hwDevCtx) { av_buffer_unref(&d->hwDevCtx); d->hwDevCtx = NULL; }
#ifdef _WIN32
    if (d->d3d11Device) { comRelease(d->d3d11Device); d->d3d11Device = NULL; }
    if (d->extDevice) { comRelease(d->extDevice); d->extDevice = NULL; }
#endif

    d->ctx = avcodec_alloc_context3(d->codec);
    if (!d->ctx) return -1;
    d->ctx->flags |= AV_CODEC_FLAG_LOW_DELAY;
    d->ctx->flags2 |= AV_CODEC_FLAG2_FAST;
    av_opt_set(d->ctx->priv_data, "threads", "1", 0);
    /* hwAccel=0：openCodec 不创建 hwDevCtx，avcodec_open2 即纯软解 */
    d->hwAccel = 0;
    d->textureOutput = 0;
    int ret = avcodec_open2(d->ctx, d->codec, NULL);
    if (ret < 0) return ret;
    return 0;
}

/* FF_IsSoftware：返回 1=已切软解（codec 已关或无 hwDevCtx），0=硬解中。 */
FFDEC_API int FF_IsSoftware(void* handle) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !d->ctx) return 1;
    if (!avcodec_is_open(d->ctx)) return 1;
    return d->hwDevCtx ? 0 : 1;
}

/* FF_NeedPLI 查询首帧 UV=0 标记的补帧需求，查询后清除（Go 侧发 PLI 补帧）。 */
FFDEC_API int FF_NeedPLI(void* handle) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d) return 0;
    int v = d->needpli;
    d->needpli = 0;
    return v;
}

FFDEC_API int FF_DecodeTexture(void* handle, const uint8_t* data, int dataLen,
                               void** outTexture, int* outSubIdx, int* outWidth, int* outHeight,
                               void** outFrame) {
#ifndef _WIN32
    return -1;
#else
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !d->ctx) return -1;
    if (!outTexture || !outWidth || !outHeight) return -1;

    if (!avcodec_is_open(d->ctx)) {
        int oret = openCodec(d);
        if (oret < 0) return oret;
    }

    d->pkt->data = (uint8_t*)data;
    d->pkt->size = dataLen;

    int ret = avcodec_send_packet(d->ctx, d->pkt);
    if (ret < 0 && ret != AVERROR(EAGAIN)) return ret;

    ret = avcodec_receive_frame(d->ctx, d->frame);
    d->frameErrorFlags = (ret == 0) ? d->frame->decode_error_flags : 0;
    if (ret == AVERROR(EAGAIN)) return 2;
    if (ret < 0) return ret;

    if (!isHwPixelFormat((enum AVPixelFormat)d->frame->format)) {
        return 4;  /* 软解帧已 receive，保留 d->frame 给 FF_ReceiveBGRA 取 BGRA（不 unref，避免重复 send_packet） */
    }

    /* 渲染器无 NV12 SRV 能力（无 D3D11.3）：硬解帧也走 BGRA 路径，避免纹理渲染失败黑屏 */
    if (!d->textureOutput) {
        return 4;  /* Go 侧调 FF_ReceiveBGRA（内部 transfer_data + sws_scale） */
    }

    /* 零拷贝：每帧独立 AVFrame（av_frame_alloc + av_frame_ref），返回句柄给 Go 侧。
       Go 侧渲染拷贝后调 FF_UnrefFrame(frameHandle) 按句柄释放，互不干扰。
       d->frame 立即 unref（独立 frame 持有 ref），解决多帧并发 use-after-free。 */
    ID3D11Texture2D* srcTex = (ID3D11Texture2D*)d->frame->data[0];
    int srcSubIdx = (int)(intptr_t)d->frame->data[1];
    int frameW = d->frame->width;
    int frameH = d->frame->height;
    if (!srcTex || frameW <= 0 || frameH <= 0 || !outFrame) {
        av_frame_unref(d->frame);
        return -1;
    }

    AVFrame* held = av_frame_alloc();
    if (!held) {
        av_frame_unref(d->frame);
        return -1;
    }
    if (av_frame_ref(held, d->frame) < 0) {
        av_frame_free(&held);
        av_frame_unref(d->frame);
        return -1;
    }
    av_frame_unref(d->frame);

    *outTexture = held->data[0];
    *outSubIdx = (int)(intptr_t)held->data[1];
    *outWidth = frameW;
    *outHeight = frameH;
    *outFrame = held;
    return 1;
#endif

}

/* FF_ReceiveTexture：只 avcodec_receive_frame、不 send_packet 的补收路径。
   FF_DecodeTexture 返回 2（EAGAIN）时 packet 已入队消费，Go 侧调本函数补收
   输出，绝不回退 FF_Decode 重复 send 同一 NALU（会扰乱参考帧 → 花屏）。
   返回：1=纹理帧（零拷贝，同 FF_DecodeTexture ret==1）、4=软解/BGRA 帧
   （调 FF_ReceiveBGRA 取）、2=仍无帧、<0=错误。 */
FFDEC_API int FF_ReceiveTexture(void* handle, void** outTexture, int* outSubIdx,
                                int* outWidth, int* outHeight, void** outFrame) {
#ifndef _WIN32
    return -1;
#else
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !d->ctx) return -1;
    if (!outTexture || !outWidth || !outHeight) return -1;

    int ret = avcodec_receive_frame(d->ctx, d->frame);
    d->frameErrorFlags = (ret == 0) ? d->frame->decode_error_flags : 0;
    if (ret == AVERROR(EAGAIN)) return 2;
    if (ret < 0) return ret;

    if (!isHwPixelFormat((enum AVPixelFormat)d->frame->format)) {
        return 4;  /* 软解帧已 receive，保留 d->frame 给 FF_ReceiveBGRA 取 BGRA */
    }

    /* 渲染器无 NV12 SRV 能力：硬解帧也走 BGRA 路径（与 FF_DecodeTexture 一致） */
    if (!d->textureOutput) {
        return 4;
    }

    /* 零拷贝：独立 AVFrame 持 ref，d->frame 立即 unref（与 FF_DecodeTexture 同款） */
    ID3D11Texture2D* srcTex = (ID3D11Texture2D*)d->frame->data[0];
    int srcSubIdx = (int)(intptr_t)d->frame->data[1];
    int frameW = d->frame->width;
    int frameH = d->frame->height;
    if (!srcTex || frameW <= 0 || frameH <= 0 || !outFrame) {
        av_frame_unref(d->frame);
        return -1;
    }

    AVFrame* held = av_frame_alloc();
    if (!held) {
        av_frame_unref(d->frame);
        return -1;
    }
    if (av_frame_ref(held, d->frame) < 0) {
        av_frame_free(&held);
        av_frame_unref(d->frame);
        return -1;
    }
    av_frame_unref(d->frame);

    *outTexture = held->data[0];
    *outSubIdx = srcSubIdx;
    *outWidth = frameW;
    *outHeight = frameH;
    *outFrame = held;
    return 1;
#endif
}

/* FF_UnrefFrame 释放独立 frame 句柄（Go 侧渲染拷贝后调用，让 hwframe 回池复用）。 */
FFDEC_API void FF_UnrefFrame(void* frame) {
    if (frame) {
        AVFrame* f = (AVFrame*)frame;
        av_frame_unref(f);
        av_frame_free(&f);
    }
}

/* FF_ReceiveBGRA: FF_DecodeTexture 返回 4 后调用，对已 receive 的软解帧转 BGRA。
   避免软解时 Go 侧重复 send_packet 同一 NALU 导致参考帧错乱马赛克。 */
FFDEC_API int FF_ReceiveBGRA(void* handle, uint8_t** outBGRA, int* outWidth, int* outHeight) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !d->ctx) return -1;
    int ret = frameToBGRA(d, d->frame, outBGRA, outWidth, outHeight);
    av_frame_unref(d->frame);
    return ret;  /* 1=成功，0=丢弃（绿帧） */
}
static int isHwPixelFormat(enum AVPixelFormat fmt) {
    switch (fmt) {
#ifdef _WIN32
    case AV_PIX_FMT_D3D11VA_VLD:
    case AV_PIX_FMT_D3D11:
    case AV_PIX_FMT_DXVA2_VLD:
        return 1;
#elif defined(__APPLE__)
    case AV_PIX_FMT_VIDEOTOOLBOX:
        return 1;
#endif
    default: return 0;
    }
}

static int openCodec(FFDecoder* d) {
    if (d->hwAccel && !d->hwDevCtx) {
#if defined(_WIN32)
        /* D3D11VA requires COM initialization on the calling thread.
           This must be done inside the DLL (not from Go) because the C
           runtime's thread-local state must be set up before D3D11CreateDevice. */
        CoInitializeEx(NULL, COINIT_MULTITHREADED);

        /* 优先用外部 device（渲染器 device）创建 hwdevice_ctx，实现解码与渲染同设备。
           mpv/VLC 标准方案：av_hwdevice_ctx_alloc + 手动设 hwctx->device + init。 */
        if (d->extDevice) {
            AVBufferRef* devCtx = av_hwdevice_ctx_alloc(av_hwdevice_find_type_by_name("d3d11va"));
            if (devCtx) {
                AVHWDeviceContext* ctx = (AVHWDeviceContext*)devCtx->data;
                AVD3D11VADeviceContext* hwctx = (AVD3D11VADeviceContext*)ctx->hwctx;
                /* AddRef：FFmpeg 清理时会 Release，不影响渲染器持有的引用 */
                ((ID3D11Device*)d->extDevice)->AddRef();
                hwctx->device = (ID3D11Device*)d->extDevice;
                int initRet = av_hwdevice_ctx_init(devCtx);
                if (initRet == 0) {
                    d->ctx->hw_device_ctx = devCtx;
                    d->hwDevCtx = devCtx;
                } else {
                    av_buffer_unref(&devCtx);
                }
            } else {
                fprintf(stderr, "[ffdecode] av_hwdevice_ctx_alloc failed\n");
            }
        }

        /* 无外部 device 或上述失败，回退到 FFmpeg 自建设备 */
        if (!d->hwDevCtx) {
            const char* hwDevs[] = { "d3d11va", "dxva2" };
            for (int i = 0; i < 2; i++) {
                AVBufferRef* devCtx = NULL;
                int ret = av_hwdevice_ctx_create(&devCtx,
                            av_hwdevice_find_type_by_name(hwDevs[i]),
                            NULL, NULL, 0);
                if (ret == 0 && devCtx) {
                    d->ctx->hw_device_ctx = devCtx;
                    d->hwDevCtx = devCtx;
                    break;
                }
            }
        }
#elif defined(__APPLE__)
        AVBufferRef* devCtx = NULL;
        int ret = av_hwdevice_ctx_create(&devCtx,
                    av_hwdevice_find_type_by_name("videotoolbox"),
                    NULL, NULL, 0);
        if (ret == 0 && devCtx) {
            d->ctx->hw_device_ctx = devCtx;
            d->hwDevCtx = devCtx;
        }
#endif
    }

    int ret = avcodec_open2(d->ctx, d->codec, NULL);
    if (ret < 0 && d->hwDevCtx) {
        av_buffer_unref(&d->hwDevCtx);
        d->ctx->hw_device_ctx = NULL;
        d->hwDevCtx = NULL;
        ret = avcodec_open2(d->ctx, d->codec, NULL);
    }
    return ret;
}

static int frameToBGRA(FFDecoder* d, AVFrame* frame,
                       uint8_t** outBGRA, int* outWidth, int* outHeight) {
    int w = frame->width;
    int h = frame->height;
    if (w <= 0 || h <= 0) return 0;

    AVFrame* srcFrame = frame;
    if (isHwPixelFormat((enum AVPixelFormat)frame->format)) {
        av_frame_unref(d->swFrame);
        int ret = av_hwframe_transfer_data(d->swFrame, frame, 0);
        if (ret < 0) return 0;
        av_frame_copy_props(d->swFrame, frame);
        srcFrame = d->swFrame;

        /* 检测 D3D11VA 未就绪的绿帧并丢弃。
           transfer_data 后若 UV plane 全 0，说明 D3D11VA 硬解纹理未初始化，
           sws_scale 按 BT.601 会算出 BGRA=[0,135,0] 纯绿帧，上屏即绿屏闪。
           丢弃这帧（return 0），渲染端无新帧则保持上一帧，用户无感。

           实证：绿帧 uv[0..15] 全 0，正常帧 UV 为 128(中性)或真实色度值。
           UV 全 0 在 limited-range NV12 是非法值，零误判。
           采样前 64 字节即可：绿帧是整帧 UV=0，前 16 字节已全 0。 */
        if (d->swFrame->data[1]) {
            uint8_t* uv = d->swFrame->data[1];
            int allZero = 1;
            for (int i = 0; i < 64; i++) {
                if (uv[i] != 0) { allZero = 0; break; }
            }
            if (allZero) {
                /* UV=0 坏帧分级处置（单帧坏帧用户就能看到灰闪，治本=丢帧保上一帧）：
                   单帧（badStreak 0→1）：直接丢弃（return 0），渲染端无新帧保持上一帧，用户无感。
                   连续 ≥2 帧：丢弃治不了（上一帧也已过期），降级为灰度输出
                   （UV memset 128，避免 sws_scale 算出纯绿）+ needpli 催好帧替换。
                   好帧到达时 badStreak 归零。 */
                d->badStreak++;
                d->needpli = 1;
                if (d->badStreak < 2) {
                    /* 单帧坏帧：不 memset（灰度不上屏），直接丢弃 */
                    if (d->swFrame->data[0]) av_frame_unref(d->swFrame);
                    return 0;
                }
                /* 连续第 2 帧起：置中性色度（灰度显示），Y 平面正常，内容仍可见 */
                size_t uvSize = (size_t)d->swFrame->linesize[1] * d->swFrame->height / 2;
                memset(d->swFrame->data[1], 128, uvSize);
            } else {
                d->badStreak = 0;
            }
        }
    }
    enum AVPixelFormat srcFmt = (enum AVPixelFormat)srcFrame->format;
    if (srcFmt < 0) return 0;

    int needSize = w * h * 4;
    if (d->bgraBufSize < needSize || d->width != w || d->height != h ||
        d->swsSrcFmt != srcFmt) {
        free(d->bgraBuf);
        d->bgraBuf = (uint8_t*)malloc(needSize);
        d->bgraBufSize = needSize;
        d->width = w;
        d->height = h;
        d->swsSrcFmt = srcFmt;

        if (d->sws) sws_freeContext(d->sws);
        d->sws = sws_getContext(w, h, srcFmt,
                                w, h, AV_PIX_FMT_BGRA,
                                SWS_BILINEAR, NULL, NULL, NULL);
        if (!d->sws) return 0;

        /* Fix green tint on Intel iGPU: D3D11VA may output limited-range BT.601
           YUV while sws_scale defaults assume the same — but the actual range
           can differ by driver. Force full-range output with standard BT.601
           coefficients to eliminate color space mismatch. */
        {
            const int *srcCoeffs = sws_getCoefficients(SWS_CS_ITU601);
            const int *dstCoeffs = sws_getCoefficients(SWS_CS_ITU601);
            sws_setColorspaceDetails(d->sws,
                srcCoeffs, 0,   /* src: BT.601, limited range */
                dstCoeffs, 1,   /* dst: BT.601, full range */
                0, 1 << 16, 1 << 16);
        }
    }

    uint8_t* dstSlice[1] = { d->bgraBuf };
    int dstStride[1] = { w * 4 };
    sws_scale(d->sws,
              (const uint8_t* const*)srcFrame->data, srcFrame->linesize,
              0, h, dstSlice, dstStride);

    /* 黑帧检测（300ms 滑动窗口占比判定，与 Go 侧 d3d11_frame.go 同策略）：
       设备端 HWC/SurfaceFlinger 异常会把纯黑帧编进 RTSP 流，与正常帧交替 → 黑/画面
       来回闪。窗口内黑帧覆盖时长 ≥25%（≥75ms）判异常 → 丢弃保上一帧（连续黑合并
       成段计覆盖时长，成对交替每段很短但占比同样超阈 → 也能拦住）；占比 <25% 放行
       （宽容：低频黑可能是真黑屏内容，如设备熄屏；孤立黑帧按帧间隔估算封顶 50ms
       不会触发阈值）。当前黑帧的时长按「与上一帧的间隔」估算（黑段长度只有下一帧
       到达才确定），封顶 50ms 防大间隔帧导致占比虚高。散布采样 64 点 BGRA
       全 <16 判黑。 */
    {
        int dark = 1, samples = 0;
        int stepX = w / 8, stepY = h / 8;
        if (stepX < 1) stepX = 1;
        if (stepY < 1) stepY = 1;
        for (int yy = 0; yy < h && samples < 64; yy += stepY) {
            for (int xx = 0; xx < w && samples < 64; xx += stepX) {
                uint8_t* px = d->bgraBuf + ((size_t)yy * w + xx) * 4;
                if (px[0] > 16 || px[1] > 16 || px[2] > 16) { dark = 0; break; }
                samples++;
            }
            if (!dark) break;
        }
        uint64_t nowMs = (uint64_t)GetTickCount64();
        /* 帧间隔（仅新开黑段估算用），封顶 BLACK_EST_CAP_MS（50ms） */
        uint64_t est = (d->blackLastMs != 0 && nowMs > d->blackLastMs)
                     ? nowMs - d->blackLastMs : 0;
        if (est > BLACK_EST_CAP_MS) est = BLACK_EST_CAP_MS;

        /* 段维护（时长口径尽量精确，估算只用于孤立黑帧）：
           连续黑段延续 → 覆盖=now-start（精确）；黑→正常 → 段闭合于本帧到达
           （精确）；新开黑段 → 时长未知，按帧间隔估算。 */
        if (dark) {
            if (d->blackLastWasBlack && d->blackSegN > 0) {
                d->blackSegs[d->blackSegN - 1].end = nowMs;
            } else {
                if (d->blackSegN == BLACK_MAX_SEGS) {
                    memmove(d->blackSegs, d->blackSegs + 1,
                            sizeof(FFBlackSeg) * (BLACK_MAX_SEGS - 1));
                    d->blackSegN--;
                }
                d->blackSegs[d->blackSegN].start = nowMs;
                d->blackSegs[d->blackSegN].end = nowMs + est;
                d->blackSegN++;
            }
        } else if (d->blackLastWasBlack && d->blackSegN > 0) {
            d->blackSegs[d->blackSegN - 1].end = nowMs;  /* 黑段精确闭合 */
        }
        d->blackLastWasBlack = dark;
        d->blackLastMs = nowMs;

        /* 窗口修剪（窗口外段丢弃）并统计窗口内黑覆盖时长 */
        while (d->blackSegN > 0 && d->blackSegs[0].end <= nowMs - BLACK_WIN_MS) {
            d->blackSegN--;
            memmove(d->blackSegs, d->blackSegs + 1, sizeof(FFBlackSeg) * d->blackSegN);
        }
        if (d->blackSegN > 0 && d->blackSegs[0].start < nowMs - BLACK_WIN_MS) {
            d->blackSegs[0].start = nowMs - BLACK_WIN_MS;  /* 跨界段裁剪起点 */
        }
        uint64_t cover = 0;
        for (int i = 0; i < d->blackSegN; i++) {
            if (d->blackSegs[i].end > d->blackSegs[i].start) {
                cover += d->blackSegs[i].end - d->blackSegs[i].start;
            }
        }
        int blackRatio = cover * 100 >= BLACK_WIN_MS * 25;  /* 覆盖 ≥75ms（25%）判异常 → 丢弃保帧 */
        if (dark) {
            d->needpli = 1;  /* 黑帧催好帧（无论放行或丢弃；正常帧不动 needpli，避免覆盖绿帧标记） */
            if (blackRatio) {
                d->blackDropped++;
                if (d->blackDropped <= 5 || d->blackDropped % 100 == 0) {
                    fprintf(stderr, "[ffdecode] black frame #%u dropped (reason=window: coverage %llums >= 25%%, hold last)\n",
                            d->blackDropped, (unsigned long long)cover);
                }
                return 0;  /* 高频黑占比：丢弃保上一帧（卡住不闪） */
            }
            d->blackShown++;
            if (d->blackShown <= 5 || d->blackShown % 100 == 0) {
                fprintf(stderr, "[ffdecode] black frame #%u shown (reason=%s: coverage %llums < 25%%, possible real black)\n",
                        d->blackShown, d->blackDropped > 0 ? "window" : "lone",
                        (unsigned long long)cover);
            }
        }
    }

    if (outBGRA) *outBGRA = d->bgraBuf;  /* NULL 检查：FF_Flush 传 NULL 时不写，避免空指针解引用 */
    if (outWidth) *outWidth = w;
    if (outHeight) *outHeight = h;
    return 1;
}

FFDEC_API int FF_Decode(void* handle,
                        const uint8_t* data, int dataLen,
                        uint8_t** outBGRA,
                        int* outWidth, int* outHeight) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !d->ctx) return -1;

    if (!avcodec_is_open(d->ctx)) {
        int oret = openCodec(d);
        fprintf(stderr, "[ffdecode] openCodec: ret=%d open=%d hwDevCtx=%p\n", oret, avcodec_is_open(d->ctx), (void*)d->hwDevCtx);
        fflush(stderr);
        if (oret < 0) return oret;
    }

    d->pkt->data = (uint8_t*)data;
    d->pkt->size = dataLen;

    int ret = avcodec_send_packet(d->ctx, d->pkt);
    if (ret < 0 && ret != AVERROR(EAGAIN)) {
        return ret;
    }

    ret = avcodec_receive_frame(d->ctx, d->frame);
    d->frameErrorFlags = (ret == 0) ? d->frame->decode_error_flags : 0;
    if (ret == AVERROR(EAGAIN)) return 0;
    if (ret < 0) return ret;

    /* 首帧策略（回退旧版行为）：无关键帧门控，收到什么渲染什么。
       FFmpeg 自行拒绝无参考帧的 P 帧（receive_frame 报错返回，Go 侧丢弃）。
 */

    int ok = frameToBGRA(d, d->frame, outBGRA, outWidth, outHeight);
    av_frame_unref(d->frame);
    if (d->swFrame->data[0]) av_frame_unref(d->swFrame);

    return ok ? 1 : 0;
}

FFDEC_API int FF_Flush(void* handle,
                       uint8_t** outBGRA,
                       int* outWidth, int* outHeight) {
    FFDecoder* d = (FFDecoder*)handle;
    if (!d || !d->ctx) return -1;

    /* drain: send_packet(NULL) drains buffered frames, then avcodec_flush_buffers
       must reset the decoder -- otherwise it stays in EOF state and every
       subsequent send_packet returns EAGAIN (decode silently stops). */
    avcodec_send_packet(d->ctx, NULL);
    while (avcodec_receive_frame(d->ctx, d->frame) == 0) {
        av_frame_unref(d->frame);
    }
    avcodec_flush_buffers(d->ctx);
    /* flush 后清黑帧窗口历史（seek/流切换后不留旧黑占比，重新宽容判定） */
    d->blackSegN = 0;
    d->blackLastWasBlack = 0;
    d->blackLastMs = 0;
    (void)outBGRA; (void)outWidth; (void)outHeight;  /* flush yields no frame */
    return 1;
}

/* ─── AAC Decoder Implementation ───────────────────────────────────────────── */

typedef struct FFAACDecoder {
    AVCodecContext*   ctx;
    const AVCodec*    codec;
    AVFrame*          frame;
    AVPacket*         pkt;
    struct SwrContext* swr;
    int               sampleRate;
    int               channels;
} FFAACDecoder;

FFDEC_API void* FF_CreateAACDecoder(int sampleRate, int channels) {
    FFAACDecoder* d = (FFAACDecoder*)calloc(1, sizeof(FFAACDecoder));
    if (!d) return NULL;

    FF_Init();

    d->sampleRate = sampleRate;
    d->channels = channels;

    d->codec = avcodec_find_decoder(AV_CODEC_ID_AAC);
    if (!d->codec) {
        free(d);
        return NULL;
    }

    d->ctx = avcodec_alloc_context3(d->codec);
    if (!d->ctx) {
        free(d);
        return NULL;
    }

    d->ctx->sample_rate = sampleRate;
    av_channel_layout_default(&d->ctx->ch_layout, channels);

    int ret = avcodec_open2(d->ctx, d->codec, NULL);
    if (ret < 0) {
        avcodec_free_context(&d->ctx);
        free(d);
        return NULL;
    }

    d->frame = av_frame_alloc();
    d->pkt = av_packet_alloc();

    return d;
}

FFDEC_API int FF_DecodeAAC(void* handle,
                           const uint8_t* data, int dataLen,
                           uint8_t* outBuf, int outBufSize) {
    FFAACDecoder* d = (FFAACDecoder*)handle;
    if (!d || !d->ctx || !outBuf) return -1;

    d->pkt->data = (uint8_t*)data;
    d->pkt->size = dataLen;

    int ret = avcodec_send_packet(d->ctx, d->pkt);
    if (ret < 0 && ret != AVERROR(EAGAIN)) {
        return ret;
    }

    ret = avcodec_receive_frame(d->ctx, d->frame);
    if (ret == AVERROR(EAGAIN)) return 0;
    if (ret < 0) return ret;

    int ch = d->frame->ch_layout.nb_channels;
    if (ch <= 0) ch = d->channels;
    int nsamples = d->frame->nb_samples;
    int pcmSize = nsamples * ch * 2;
    if (pcmSize > outBufSize) pcmSize = outBufSize;

    if (!d->swr) {
        int frameSampleRate = d->frame->sample_rate;
        if (frameSampleRate <= 0) frameSampleRate = d->sampleRate;

        AVChannelLayout outCL;
        av_channel_layout_default(&outCL, ch);
        AVChannelLayout inCL;
        av_channel_layout_copy(&inCL, &d->frame->ch_layout);
        swr_alloc_set_opts2(&d->swr,
                                &outCL, AV_SAMPLE_FMT_S16, frameSampleRate,
                                &inCL, (AVSampleFormat)d->frame->format, frameSampleRate,
                                0, NULL);
        av_channel_layout_uninit(&outCL);
        av_channel_layout_uninit(&inCL);
        if (d->swr) swr_init(d->swr);
    }

    if (d->swr) {
        uint8_t* dst[1] = { outBuf };
        int converted = swr_convert(d->swr,
                                    dst, nsamples,
                                    (const uint8_t **)d->frame->extended_data,
                                    nsamples);
        if (converted > 0) {
            pcmSize = converted * ch * 2;
            if (pcmSize > outBufSize) pcmSize = outBufSize;
        }
    } else {
        int srcFmt = d->frame->format;
        if (srcFmt == AV_SAMPLE_FMT_S16) {
            int avail = d->frame->linesize[0];
            if (avail > pcmSize) avail = pcmSize;
            memcpy(outBuf, d->frame->data[0], avail);
            pcmSize = avail;
        } else if (srcFmt == AV_SAMPLE_FMT_S16P) {
            int16_t* out = (int16_t*)outBuf;
            int maxSamples = outBufSize / (ch * 2);
            int s = nsamples < maxSamples ? nsamples : maxSamples;
            for (int i = 0; i < s; i++) {
                for (int c = 0; c < ch; c++) {
                    *out++ = ((int16_t*)d->frame->data[c])[i];
                }
            }
            pcmSize = s * ch * 2;
        } else {
            pcmSize = 0;
        }
    }

    av_frame_unref(d->frame);
    return pcmSize;
}

FFDEC_API void FF_DestroyAACDecoder(void* handle) {
    FFAACDecoder* d = (FFAACDecoder*)handle;
    if (!d) return;

    if (d->swr) swr_free(&d->swr);
    if (d->frame) av_frame_free(&d->frame);
    if (d->pkt) av_packet_free(&d->pkt);
    if (d->ctx) avcodec_free_context(&d->ctx);
    free(d);
}
