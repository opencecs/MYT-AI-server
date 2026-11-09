// metalrender.m — Metal GPU renderer for macOS
// Exposes C API matching D3D11R_* interface pattern.
// Build: clang -std=c11 -ObjC -shared -O2 -o metalrender.dylib metalrender.m -framework Metal -framework MetalKit -framework AppKit -framework CoreVideo

#import <Metal/Metal.h>
#import <AppKit/AppKit.h>
#import <QuartzCore/CAMetalLayer.h>
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>

// ─── Public C API ─────────────────────────────────────────────────────────

#define METALR_API __attribute__((visibility("default")))

METALR_API void*  MetalR_Create(void* nsView);
METALR_API void   MetalR_Destroy(void* handle);
METALR_API int    MetalR_RenderBGRA(void* handle, const void* bgraData, int frameW, int frameH, int stride);
METALR_API void   MetalR_Present(void* handle);
METALR_API void   MetalR_RenderIdle(void* handle);
METALR_API void   MetalR_Resize(void* handle, int w, int h);
METALR_API void   MetalR_SetRotation(void* handle, int rotated);
METALR_API void   MetalR_GetLetterbox(void* handle, int* drawW, int* drawH, int* offX, int* offY);

// ─── Internal Types ────────────────────────────────────────────────────────

typedef struct {
    id<MTLDevice>        device;
    id<MTLCommandQueue>  commandQueue;
    id<MTLRenderPipelineState> pipeline;
    id<MTLRenderPipelineState> pipelineRotated;
    id<MTLBuffer>        vertexBuf;
    CAMetalLayer*        metalLayer;
    NSView*              nsView;

    // winW/winH: drawable size = view.bounds (full MetalView area including NavBar zone)
    // Used by updateVertices for NDC normalization
    int winW, winH;

    // videoW/videoH: visible video area (excluding NavBar)
    // Used by computeLetterbox for centering the video
    int videoW, videoH;

    int srcW, srcH;
    int rotated;

    // Letterbox geometry (relative to winW/winH drawable)
    int lbDrawW, lbDrawH, lbOffX, lbOffY;

    // BGRA upload texture
    id<MTLTexture> bgraTex;
    int texW, texH;

    // Pending frame flag
    atomic_int pendingFrame;

    // Sampler
    id<MTLSamplerState> sampler;
} MetalRenderer;

// ─── Shaders ──────────────────────────────────────────────────────────────

static const char* shaderSource =
    "#include <metal_stdlib>\n"
    "using namespace metal;\n"
    "\n"
    "struct VertexOut {\n"
    "    float4 pos [[position]];\n"
    "    float2 uv;\n"
    "};\n"
    "\n"
    "vertex VertexOut vs_default(uint vid [[vertex_id]], constant float* verts [[buffer(0)]]) {\n"
    "    VertexOut o;\n"
    "    o.pos = float4(verts[vid*4], verts[vid*4+1], 0, 1);\n"
    "    o.uv  = float2(verts[vid*4+2], verts[vid*4+3]);\n"
    "    return o;\n"
    "}\n"
    "\n"
    "fragment float4 ps_bgra(VertexOut in [[stage_in]], texture2d<float> tex [[texture(0)]], sampler s [[sampler(0)]]) {\n"
    "    return tex.sample(s, in.uv);\n"
    "}\n";

// ─── Letterbox ────────────────────────────────────────────────────────────

static void computeLetterbox(MetalRenderer* r) {
    // Letterbox centers the video within videoW×videoH,
    // then offsets to position it at the top of the winW×winH drawable.
    int videoW = r->videoW, videoH = r->videoH;
    if (videoW <= 0) videoW = r->winW;
    if (videoH <= 0) videoH = r->winH;

    int srcW = r->srcW, srcH = r->srcH;
    int effW = srcW, effH = srcH;

    if (r->rotated && srcW > 0 && srcH > 0 && srcW >= srcH) {
        effW = srcH; effH = srcW;
    }

    int drawW = 0, drawH = 0, offX = 0, offY = 0;
    if (effW > 0 && effH > 0) {
        if (videoW * effH > videoH * effW) {
            drawH = videoH;
            drawW = videoH * effW / effH;
            offX = (videoW - drawW) / 2;
        } else {
            drawW = videoW;
            drawH = videoW * effH / effW;
            offY = (videoH - drawH) / 2;
        }
    }
    r->lbDrawW = drawW; r->lbDrawH = drawH;
    r->lbOffX = offX;  r->lbOffY = offY;
}

// ─── Vertex Buffer ────────────────────────────────────────────────────────

static void updateVertices(MetalRenderer* r) {
    int winW = r->winW, winH = r->winH;
    if (winW <= 0 || winH <= 0) return;

    // Letterbox offsets are computed relative to videoW×videoH (top of drawable).
    // Normalize directly by drawable size — video stays at the top, NavBar covers the bottom.
    float drawW = (float)r->lbDrawW / winW;
    float drawH = (float)r->lbDrawH / winH;
    float offX  = (float)r->lbOffX  / winW;
    float offY  = (float)r->lbOffY  / winH;

    // NDC: left=-1..right=+1, top=+1..bottom=-1
    float left   = -1.0f + offX * 2.0f;
    float right  = left + drawW * 2.0f;
    float top    =  1.0f - offY * 2.0f;
    float bottom = top - drawH * 2.0f;

    // Triangle topology: TL, BL, TR, BL, BR, TR
    float verts[24];
    if (!r->rotated || r->srcW < r->srcH) {
        float v[24] = {
            left,  top,    0, 0,
            left,  bottom, 0, 1,
            right, top,    1, 0,
            left,  bottom, 0, 1,
            right, bottom, 1, 1,
            right, top,    1, 0,
        };
        memcpy(verts, v, sizeof(v));
    } else {
        float v[24] = {
            left,  top,    0, 1,
            left,  bottom, 1, 1,
            right, top,    0, 0,
            left,  bottom, 1, 1,
            right, bottom, 1, 0,
            right, top,    0, 0,
        };
        memcpy(verts, v, sizeof(v));
    }

    memcpy([r->vertexBuf contents], verts, sizeof(verts));
}

// ─── BGRA Texture ─────────────────────────────────────────────────────────

static id<MTLTexture> getBgraTexture(MetalRenderer* r, int w, int h) {
    @autoreleasepool {
        if (r->bgraTex && r->texW == w && r->texH == h) {
            return r->bgraTex;
        }

        MTLTextureDescriptor* desc = [[MTLTextureDescriptor alloc] init];
        desc.textureType = MTLTextureType2D;
        desc.pixelFormat = MTLPixelFormatBGRA8Unorm;
        desc.width = w;
        desc.height = h;
        desc.usage = MTLTextureUsageShaderRead;
        desc.storageMode = MTLStorageModeManaged;

        r->bgraTex = [r->device newTextureWithDescriptor:desc];
        r->texW = w;
        r->texH = h;
        return r->bgraTex;
    }
}

// ─── Create / Destroy ─────────────────────────────────────────────────────

METALR_API void* MetalR_Create(void* nsView) {
    @autoreleasepool {
        NSView* view = (__bridge NSView*)nsView;
        if (!view) return NULL;

        id<MTLDevice> device = MTLCreateSystemDefaultDevice();
        if (!device) {
            fprintf(stderr, "[metalrender] No Metal device found\n");
            return NULL;
        }

        MetalRenderer* r = (MetalRenderer*)calloc(1, sizeof(MetalRenderer));
        r->device = device;
        r->nsView = view;
        r->commandQueue = [device newCommandQueue];

        // Setup CAMetalLayer on the view
        view.wantsLayer = YES;
        CAMetalLayer* layer = [CAMetalLayer layer];
        layer.device = device;
        layer.pixelFormat = MTLPixelFormatBGRA8Unorm;
        layer.framebufferOnly = YES;
        layer.frame = view.bounds;
        layer.opaque = YES;
        layer.backgroundColor = [NSColor blackColor].CGColor;
        view.layer = layer;
        r->metalLayer = layer;

        // Read initial size and set drawable size
        NSRect frame = view.bounds;
        r->winW = (int)frame.size.width;
        r->winH = (int)frame.size.height;
        r->videoW = r->winW;
        r->videoH = r->winH;
        layer.drawableSize = CGSizeMake(frame.size.width, frame.size.height);

        // Compile shaders
        NSError* err = nil;
        id<MTLLibrary> lib = [device newLibraryWithSource:[NSString stringWithUTF8String:shaderSource]
                                                 options:nil
                                                   error:&err];
        if (!lib) {
            fprintf(stderr, "[metalrender] Shader compile error: %s\n", err.localizedDescription.UTF8String);
            free(r);
            return NULL;
        }

        id<MTLFunction> vs = [lib newFunctionWithName:@"vs_default"];
        id<MTLFunction> ps = [lib newFunctionWithName:@"ps_bgra"];

        MTLRenderPipelineDescriptor* pd = [[MTLRenderPipelineDescriptor alloc] init];
        pd.vertexFunction = vs;
        pd.fragmentFunction = ps;
        pd.colorAttachments[0].pixelFormat = MTLPixelFormatBGRA8Unorm;

        r->pipeline = [device newRenderPipelineStateWithDescriptor:pd error:&err];
        if (!r->pipeline) {
            fprintf(stderr, "[metalrender] Pipeline error: %s\n", err.localizedDescription.UTF8String);
            free(r);
            return NULL;
        }

        // Sampler
        MTLSamplerDescriptor* sd = [[MTLSamplerDescriptor alloc] init];
        sd.minFilter = MTLSamplerMinMagFilterLinear;
        sd.magFilter = MTLSamplerMinMagFilterLinear;
        sd.mipFilter = MTLSamplerMipFilterNotMipmapped;
        sd.sAddressMode = MTLSamplerAddressModeClampToEdge;
        sd.tAddressMode = MTLSamplerAddressModeClampToEdge;
        r->sampler = [device newSamplerStateWithDescriptor:sd];

        // Vertex buffer
        r->vertexBuf = [device newBufferWithLength:24 * sizeof(float) options:MTLResourceStorageModeShared];

        computeLetterbox(r);
        updateVertices(r);

        return r;
    }
}

METALR_API void MetalR_Destroy(void* handle) {
    if (!handle) return;
    MetalRenderer* r = (MetalRenderer*)handle;
    @autoreleasepool {
        r->bgraTex = nil;
        r->vertexBuf = nil;
        r->pipeline = nil;
        r->pipelineRotated = nil;
        r->sampler = nil;
        r->commandQueue = nil;
        r->device = nil;
        r->metalLayer = nil;
    }
    free(r);
}

// ─── Render BGRA ──────────────────────────────────────────────────────────

METALR_API int MetalR_RenderBGRA(void* handle, const void* bgraData, int frameW, int frameH, int stride) {
    if (!handle || !bgraData || frameW <= 0 || frameH <= 0) return -1;
    MetalRenderer* r = (MetalRenderer*)handle;

    @autoreleasepool {
        r->srcW = frameW;
        r->srcH = frameH;
        computeLetterbox(r);
        updateVertices(r);

        // Upload BGRA to texture
        id<MTLTexture> tex = getBgraTexture(r, frameW, frameH);
        if (!tex) return -1;

        [tex replaceRegion:MTLRegionMake2D(0, 0, frameW, frameH)
               mipmapLevel:0
                 withBytes:bgraData
               bytesPerRow:stride];

        atomic_store(&r->pendingFrame, 1);
    }
    return 0;
}

// ─── Present ──────────────────────────────────────────────────────────────

METALR_API void MetalR_Present(void* handle) {
    if (!handle) return;
    MetalRenderer* r = (MetalRenderer*)handle;

    if (!atomic_exchange(&r->pendingFrame, 0)) return;

    @autoreleasepool {
        id<CAMetalDrawable> drawable = [r->metalLayer nextDrawable];
        if (!drawable) return;

        id<MTLCommandBuffer> cmd = [r->commandQueue commandBuffer];

        MTLRenderPassDescriptor* rpd = [[MTLRenderPassDescriptor alloc] init];
        rpd.colorAttachments[0].texture = drawable.texture;
        rpd.colorAttachments[0].loadAction = MTLLoadActionClear;
        rpd.colorAttachments[0].clearColor = MTLClearColorMake(0, 0, 0, 1);
        rpd.colorAttachments[0].storeAction = MTLStoreActionStore;

        id<MTLRenderCommandEncoder> enc = [cmd renderCommandEncoderWithDescriptor:rpd];
        [enc setRenderPipelineState:r->pipeline];
        [enc setVertexBuffer:r->vertexBuf offset:0 atIndex:0];
        [enc setFragmentTexture:(__bridge id<MTLTexture>)r->bgraTex atIndex:0];
        [enc setFragmentSamplerState:r->sampler atIndex:0];
        [enc drawPrimitives:MTLPrimitiveTypeTriangle vertexStart:0 vertexCount:6];
        [enc endEncoding];

        [cmd presentDrawable:drawable];
        [cmd commit];
    }
}

// ─── RenderIdle ───────────────────────────────────────────────────────────

METALR_API void MetalR_RenderIdle(void* handle) {
    if (!handle) return;
    MetalRenderer* r = (MetalRenderer*)handle;

    @autoreleasepool {
        id<CAMetalDrawable> drawable = [r->metalLayer nextDrawable];
        if (!drawable) return;

        id<MTLCommandBuffer> cmd = [r->commandQueue commandBuffer];

        MTLRenderPassDescriptor* rpd = [[MTLRenderPassDescriptor alloc] init];
        rpd.colorAttachments[0].texture = drawable.texture;
        rpd.colorAttachments[0].loadAction = MTLLoadActionClear;
        rpd.colorAttachments[0].clearColor = MTLClearColorMake(0, 0, 0, 1);
        rpd.colorAttachments[0].storeAction = MTLStoreActionStore;

        id<MTLRenderCommandEncoder> enc = [cmd renderCommandEncoderWithDescriptor:rpd];
        [enc endEncoding];

        [cmd presentDrawable:drawable];
        [cmd commit];
    }
}

// ─── Resize ───────────────────────────────────────────────────────────────

// w/h: video area size (excluding NavBar). Drawable size is derived from view.bounds.
METALR_API void MetalR_Resize(void* handle, int w, int h) {
    if (!handle) return;
    MetalRenderer* r = (MetalRenderer*)handle;

    // videoW/videoH is the visible video area (may exclude NavBar)
    r->videoW = w;
    r->videoH = h;

    @autoreleasepool {
        if (r->metalLayer && r->nsView) {
            // Drawable/layer must match the full MetalView bounds
            NSRect bounds = r->nsView.bounds;
            r->winW = (int)bounds.size.width;
            r->winH = (int)bounds.size.height;
            r->metalLayer.drawableSize = bounds.size;
            r->metalLayer.frame = bounds;
        }
    }

    computeLetterbox(r);
    updateVertices(r);
}

// ─── SetRotation ──────────────────────────────────────────────────────────

METALR_API void MetalR_SetRotation(void* handle, int rotated) {
    if (!handle) return;
    MetalRenderer* r = (MetalRenderer*)handle;
    r->rotated = rotated;
    computeLetterbox(r);
    updateVertices(r);
}

// ─── GetLetterbox ─────────────────────────────────────────────────────────

METALR_API void MetalR_GetLetterbox(void* handle, int* drawW, int* drawH, int* offX, int* offY) {
    if (!handle) return;
    MetalRenderer* r = (MetalRenderer*)handle;
    if (drawW) *drawW = r->lbDrawW;
    if (drawH) *drawH = r->lbDrawH;
    if (offX)  *offX  = r->lbOffX;
    if (offY)  *offY  = r->lbOffY;
}
