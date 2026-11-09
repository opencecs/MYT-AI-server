/*
 * d3d11render.cpp — Direct3D 11 GPU video renderer DLL.
 *
 * Renders BGRA textures to a window via D3D11.
 * D3D11R_RenderBGRA: accepts raw BGRA CPU pixels, uploads to GPU, renders.
 * D3D11R_CopyTexture / D3D11R_RenderSlot: 双槽 copyTex 管线（mpv 式）。
 *   - 解码线程调用 D3D11R_CopyTexture 把 FFmpeg hwframe 纹理拷入本渲染器的 copyTex 槽位，
 *     拷贝命令提交到同一 immediate context 后即可释放源帧（GPU 串行执行保证安全）。
 *   - UI 线程调用 D3D11R_RenderSlot 只做 Draw，不接触解码器纹理。
 *   - 双槽轮换：解码写 A 时 UI 画 B，同一 immediate context 串行化保证无撕裂。
 * D3D11R_Represent: 兜底重绘最近一帧并 Present（DWM surface 失效/遮挡恢复场景）。
 *
 * Build (Windows, MSVC):
 *   cl /LD /utf-8 /O2 /MT d3d11render.cpp /link d3d11.lib dxgi.lib d3dcompiler.lib /OUT:d3d11render.dll
 */

#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdio.h>
#include <stdarg.h>
#include <exception>

#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#define NTDDI_VERSION NTDDI_WIN10
#include <windows.h>
#include <d3d11.h>
#include <d3d11_4.h>
#include <dxgi1_2.h>
#include <d3dcompiler.h>
#endif

#ifdef _WIN32
  #define R_API __declspec(dllexport)
  /* Forward declaration: logMsg 定义在后面，但早期函数（如 computeLetterbox）调用它 */
  static void logMsg(const char* fmt, ...);
#else
  #define R_API __attribute__((visibility("default")))
#endif

/* ─── HLSL Shaders ────────────────────────────────────────────────────────── */

static const char vsSource[] =
    "struct VSInput { float2 pos : POSITION; float2 uv : TEXCOORD; };"
    "struct VSOutput { float4 pos : SV_POSITION; float2 uv : TEXCOORD; };"
    "VSOutput main(VSInput i) { VSOutput o; o.pos = float4(i.pos, 0, 1); o.uv = i.uv; return o; }";

static const char psSource[] =
    "Texture2D<float4> tex : register(t0);"
    "SamplerState samp : register(s0);"
    "struct PSInput { float4 pos : SV_POSITION; float2 uv : TEXCOORD; };"
    "float4 main(PSInput i) : SV_TARGET {"
    "  return tex.Sample(samp, i.uv);"
    "}";

/* NV12 pixel shader：业界做法——GPU 硬解零拷贝直渲染，不做逐帧 CPU 回读检测
   （mpv/VLC 同款；frameClassify 的 Map stall 是 GPU 优势元凶，已删）。
   UV=0 中性修复：D3D11VA 偶发吐 UV=0 半帧时，逐像素视为中性色度（灰度显示）
   而非纯绿。limited-range NV12 合法 UV 不为 0，正常帧零误伤。零 CPU 开销。 */
static const char nv12PsSource[] =
    "Texture2DArray<float> yPlane : register(t0);"
    "Texture2DArray<float2> uvPlane : register(t1);"
    "SamplerState samp : register(s0);"
    "struct PSInput { float4 pos : SV_POSITION; float2 uv : TEXCOORD; };"
    "float4 main(PSInput i) : SV_TARGET {"
    "  float y = yPlane.Sample(samp, float3(i.uv, 0));"
    "  float2 uv = uvPlane.Sample(samp, float3(i.uv, 0));"
    "  if (uv.x < 0.01 && uv.y < 0.01) { uv = float2(0.50196, 0.50196); }"
    "  float c = (y - 0.0627) * 1.164383;"
    "  float d = uv.y - 0.50196;"
    "  float e = uv.x - 0.50196;"
    "  float r = c + 1.596027 * e;"
    "  float g = c - 0.391762 * d - 0.812968 * e;"
    "  float b = c + 2.017232 * d;"
    "  return float4(b, g, r, 1.0);"
    "}";

/* ─── Renderer State ──────────────────────────────────────────────────────── */

typedef struct {
    CRITICAL_SECTION         cs;       /* protects all D3D11 operations */
    ID3D11Device*            device;
    ID3D11Device3*           device3;  /* D3D11.3 for NV12 plane SRV1（PlaneSlice） */
    ID3D11DeviceContext*     ctx;
    ID3D11DeviceContext1*    ctx1;     /* D3D11.1 for CopySubresourceRegion1（COPY_DISCARD）防闪屏 */
    IDXGISwapChain1*         swapChain;
    ID3D11RenderTargetView*  rtv;
    ID3D11VertexShader*      vs;
    ID3D11PixelShader*       ps;
    ID3D11PixelShader*       nv12ps;
    ID3D11InputLayout*       inputLayout;
    ID3D11SamplerState*      sampler;
    ID3D11Buffer*            vertexBuf;
    /* BGRA upload texture + 缓存 SRV（尺寸变化才重建） */
    ID3D11Texture2D*         bgraTex;
    ID3D11ShaderResourceView* srvBgra;
    int                      bgraTexW, bgraTexH;
    HWND                     hWnd;
    int                      width;    /* swapchain 尺寸（仅由 Resize/创建时更新） */
    int                      height;
    int                      frameW;
    int                      frameH;
    int                      rotated;
    int                      fill;     /* 1=stretch fill (skip letterbox aspect) */
    int                      isWarp;   /* 1=device 是 WARP 软光栅（无 GPU），解码器不应复用做 D3D11VA 硬解 */
    int                      lbStale;
    int                      srcW, srcH;
    int                      drawW, drawH, offX, offY;
    volatile long            pendingFrame;   /* 1 = new frame ready to present */
    volatile long            hasEverRendered;/* 1 = at least one frame has been drawn successfully */
    /* 双槽 copyTex：解码线程拷入、UI 线程绘制，轮换使用（mpv copy_tex 模式）
       每槽独立记录尺寸（分辨率变化时两槽都要重建，共用全局尺寸会导致第二槽
       尺寸不匹配时误判“已匹配”不重建 → 拷贝失败 → 黑屏/花屏） */
    ID3D11Texture2D*         copyTex[2];
    ID3D11ShaderResourceView* srvCopyY[2];
    ID3D11ShaderResourceView* srvCopyUV[2];
    int                      copyTexW[2], copyTexH[2];
    int                      copySlot;       /* 最近一次拷入的槽位（-1=尚无） */
    /* 纹理回读 staging：单例惰性重建（NV12，D3D11_USAGE_STAGING + CPU_ACCESS_READ，
       BindFlags=0，mpv ra_d3d11.c:492-505 同款）。screenshot 控制接口
      （HTTP goroutine）经 D3D11R_ReadSlot 用，与 Present/解码互斥于 cs。 */
    ID3D11Texture2D*         stagingTex;
    int                      stagingW, stagingH;
    /* UV 坏帧探针 staging：64x4 微小 NV12 STAGING 纹理（DecodeTexture 后 64 点
       UV 采样判 UV=0 损坏帧用，单帧丢弃保上一帧）。与 stagingTex 分开（尺寸恒定
       64x4 不随帧变），惰性创建一次复用。 */
    ID3D11Texture2D*         uvProbeTex;
    /* lumaProbeTex：亮度+色度联合探针（NV12 64x12 STAGING，采样 Y 上/中/下
       3 条带各 4 行 + UV 顶部 4 行）。黑帧检测用：Y 暗点占比 ≥95% 判黑、
       UV 全 0 或全 128 佐证异常帧，区分「码流真黑帧」与「UV=0 坏帧」。
       惰性创建一次复用。 */
    ID3D11Texture2D*         lumaProbeTex;
    int                      drawnSlot;      /* 最近一次绘制的槽位（-1=尚无） */
    volatile ULONGLONG       lastDrawTick;   /* 最近一次 Draw 的 tick（Represent 节流） */
} D3D11Renderer;

/* ─── Letterbox ──────────────────────────────────────────────────────────── */

static void computeLetterbox(D3D11Renderer* r) {
    if (!r->lbStale) return;
    r->lbStale = 0;

    /* Letterbox 几何一律按 HWND 当前客户区计算（不写回 r->width/height）：
       r->width/height 是 swapchain 尺寸，拖动中（轻量 resize）两者可能不同步，
       写回会导致 viewport 与 RTV 失配 → 黑边/黑闪。 */
    int winW = r->width, winH = r->height;
    RECT rc;
    if (r->hWnd && GetClientRect(r->hWnd, &rc)) {
        winW = (int)(rc.right - rc.left);
        winH = (int)(rc.bottom - rc.top);
    }
    /* fill 模式：拉伸填满整个客户区（跳过比例计算，轻微变形）。
       单台投屏对齐壳子画面比例（480:798）用，批量投屏不开启。 */
    if (r->fill) {
        r->drawW = winW; r->drawH = winH; r->offX = 0; r->offY = 0;
        return;
    }
    int effW = r->srcW, effH = r->srcH;
    if (r->rotated && r->srcW >= r->srcH) { effW = r->srcH; effH = r->srcW; }

    r->drawW = 0; r->drawH = 0; r->offX = 0; r->offY = 0;
    if (effW <= 0 || effH <= 0) return;

    if (winW * effH > winH * effW) {
        r->drawH = winH;
        r->drawW = (winH * effW + effH / 2) / effH;
        r->offX = (winW - r->drawW) / 2;
    } else {
        r->drawW = winW;
        r->drawH = (winW * effH + effW / 2) / effW;
        r->offY = (winH - r->drawH) / 2;
    }
    /* 黑边 ≤2 像素时直接填满，消除整数截断残余黑边（1-2px 宽高比偏差肉眼不可见） */
    if (r->offX > 0 && r->offX <= 2) { r->drawW = winW; r->offX = 0; }
    if (r->offY > 0 && r->offY <= 2) { r->drawH = winH; r->offY = 0; }

    logMsg("[DLL lb] win=%dx%d src=%dx%d eff=%dx%d draw=%dx%d off=%d,%d rot=%d",
           winW, winH, r->srcW, r->srcH, effW, effH,
           r->drawW, r->drawH, r->offX, r->offY, r->rotated);
}

/* ─── Vertex buffer for fullscreen quad ──────────────────────────────────── */

typedef struct { float x, y, u, v; } Vertex;

static void updateVertices(D3D11Renderer* r) {
    if (!r->vertexBuf || !r->ctx) return;

    computeLetterbox(r);
    /* 顶点按窗口客户区计算 NDC；swapchain 尺寸与窗口不一致时由 DWM 拉伸适配 */
    int winW = r->width, winH = r->height;
    RECT rc;
    if (r->hWnd && GetClientRect(r->hWnd, &rc)) {
        winW = (int)(rc.right - rc.left);
        winH = (int)(rc.bottom - rc.top);
    }
    if (winW <= 0 || winH <= 0) return;

    float drawW = (float)r->drawW, drawH = (float)r->drawH;
    float offX = (float)r->offX, offY = (float)r->offY;

    float l = offX / winW * 2.0f - 1.0f;
    float ri = (offX + drawW) / winW * 2.0f - 1.0f;
    float t = -offY / winH * 2.0f + 1.0f;
    float b = -(offY + drawH) / winH * 2.0f + 1.0f;

    Vertex verts[6];
    if (!r->rotated || r->srcW < r->srcH) {
        verts[0] = {l, t, 0, 0};
        verts[1] = {ri, t, 1, 0};
        verts[2] = {l, b, 0, 1};
        verts[3] = {ri, t, 1, 0};
        verts[4] = {ri, b, 1, 1};
        verts[5] = {l, b, 0, 1};
    } else {
        /* 90° clockwise rotation: landscape → portrait */
        verts[0] = {l, t, 0, 1};
        verts[1] = {ri, t, 0, 0};
        verts[2] = {l, b, 1, 1};
        verts[3] = {ri, t, 0, 0};
        verts[4] = {ri, b, 1, 0};
        verts[5] = {l, b, 1, 1};
    }

    D3D11_MAPPED_SUBRESOURCE ms;
    HRESULT hr = r->ctx->Map(r->vertexBuf, 0, D3D11_MAP_WRITE_DISCARD, 0, &ms);
    if (SUCCEEDED(hr)) {
        memcpy(ms.pData, verts, sizeof(verts));
        r->ctx->Unmap(r->vertexBuf, 0);
    }
}

/* ─── Get or create BGRA upload texture（SRV 缓存，不再每帧创建） ─────────── */

static ID3D11Texture2D* getBgraTexture(D3D11Renderer* r, int w, int h) {
    if (r->bgraTex && r->bgraTexW == w && r->bgraTexH == h) {
        return r->bgraTex;
    }
    if (r->bgraTex) {
        r->bgraTex->Release();
        r->bgraTex = NULL;
    }
    if (r->srvBgra) {
        r->srvBgra->Release();
        r->srvBgra = NULL;
    }

    D3D11_TEXTURE2D_DESC desc = {0};
    desc.Width = (UINT)w;
    desc.Height = (UINT)h;
    desc.MipLevels = 1;
    desc.ArraySize = 1;
    desc.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
    desc.SampleDesc.Count = 1;
    desc.Usage = D3D11_USAGE_DYNAMIC;
    desc.BindFlags = D3D11_BIND_SHADER_RESOURCE;
    desc.CPUAccessFlags = D3D11_CPU_ACCESS_WRITE;

    HRESULT hr = r->device->CreateTexture2D(&desc, NULL, &r->bgraTex);
    if (FAILED(hr)) {
        logMsg("[d3d11render] CreateTexture2D(DYNAMIC) failed: hr=0x%08X\n", (unsigned)hr);
        return NULL;
    }

    D3D11_SHADER_RESOURCE_VIEW_DESC srvDesc;
    ZeroMemory(&srvDesc, sizeof(srvDesc));
    srvDesc.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
    srvDesc.ViewDimension = D3D11_SRV_DIMENSION_TEXTURE2D;
    srvDesc.Texture2D.MipLevels = 1;
    r->device->CreateShaderResourceView(r->bgraTex, &srvDesc, &r->srvBgra);

    r->bgraTexW = w;
    r->bgraTexH = h;
    return r->bgraTex;
}

/* ─── Init renderer resources ────────────────────────────────────────────── */

static int initRenderer(D3D11Renderer* r) {
    /* Create swap chain via DXGI */
    IDXGIDevice* dxgiDev = NULL;
    HRESULT hr = r->device->QueryInterface(__uuidof(IDXGIDevice), (void**)&dxgiDev);
    if (FAILED(hr)) {
        logMsg("[d3d11render] initRenderer: QI IDXGIDevice failed: hr=0x%08X\n", (unsigned)hr);
        return -1;
    }

    IDXGIAdapter* adapter = NULL;
    dxgiDev->GetAdapter(&adapter);
    dxgiDev->Release();

    IDXGIFactory2* factory = NULL;
    adapter->GetParent(__uuidof(IDXGIFactory2), (void**)&factory);
    adapter->Release();

    DXGI_SWAP_CHAIN_DESC1 scd = {0};
    scd.Width = (UINT)r->width;
    scd.Height = (UINT)r->height;
    scd.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
    scd.SampleDesc.Count = 1;
    scd.BufferCount = 2;
    scd.SwapEffect = DXGI_SWAP_EFFECT_FLIP_DISCARD;
    scd.BufferUsage = DXGI_USAGE_RENDER_TARGET_OUTPUT;

    hr = factory->CreateSwapChainForHwnd(r->device, r->hWnd, &scd, NULL, NULL, &r->swapChain);
    logMsg("[initRenderer] CreateSwapChain hr=0x%x swapChain=0x%p %dx%d\n", (unsigned)hr, (void*)r->swapChain, r->width, r->height);
    factory->Release();
    if (FAILED(hr)) {
        logMsg("[d3d11render] CreateSwapChainForHwnd failed: hr=0x%08X\n", (unsigned)hr);
        return -1;
    }

    /* Create RTV */
    ID3D11Texture2D* backBuf = NULL;
    r->swapChain->GetBuffer(0, __uuidof(ID3D11Texture2D), (void**)&backBuf);
    r->device->CreateRenderTargetView(backBuf, NULL, &r->rtv);
    logMsg("[initRenderer] RTV rtv=0x%p\n", (void*)r->rtv);
    backBuf->Release();

    /* Compile shaders */
    ID3DBlob* vsBlob = NULL, *psBlob = NULL;
    D3DCompile(vsSource, sizeof(vsSource)-1, NULL, NULL, NULL, "main", "vs_5_0", 0, 0, &vsBlob, NULL);
    D3DCompile(psSource, sizeof(psSource)-1, NULL, NULL, NULL, "main", "ps_5_0", 0, 0, &psBlob, NULL);

    r->device->CreateVertexShader(vsBlob->GetBufferPointer(), vsBlob->GetBufferSize(), NULL, &r->vs);
    r->device->CreatePixelShader(psBlob->GetBufferPointer(), psBlob->GetBufferSize(), NULL, &r->ps);
    logMsg("[initRenderer] shaders vs=0x%p ps=0x%p\n", (void*)r->vs, (void*)r->ps);

    /* Compile NV12 pixel shader for zero-copy D3D11VA path */
    {
        ID3DBlob* nv12Blob = NULL;
        D3DCompile(nv12PsSource, sizeof(nv12PsSource)-1, NULL, NULL, NULL, "main", "ps_5_0", 0, 0, &nv12Blob, NULL);
        if (nv12Blob) {
            r->device->CreatePixelShader(nv12Blob->GetBufferPointer(), nv12Blob->GetBufferSize(), NULL, &r->nv12ps);
            logMsg("[initRenderer] nv12ps=0x%p\n", (void*)r->nv12ps);
            nv12Blob->Release();
        }
    }

    D3D11_INPUT_ELEMENT_DESC ied[] = {
        {"POSITION", 0, DXGI_FORMAT_R32G32_FLOAT, 0, 0, D3D11_INPUT_PER_VERTEX_DATA, 0},
        {"TEXCOORD", 0, DXGI_FORMAT_R32G32_FLOAT, 0, 8, D3D11_INPUT_PER_VERTEX_DATA, 0},
    };
    r->device->CreateInputLayout(ied, 2, vsBlob->GetBufferPointer(), vsBlob->GetBufferSize(), &r->inputLayout);
    vsBlob->Release();
    psBlob->Release();

    /* Sampler */
    D3D11_SAMPLER_DESC sd = {0};
    sd.Filter = D3D11_FILTER_MIN_MAG_LINEAR_MIP_POINT;
    sd.AddressU = sd.AddressV = sd.AddressW = D3D11_TEXTURE_ADDRESS_CLAMP;
    r->device->CreateSamplerState(&sd, &r->sampler);

    /* Dynamic vertex buffer */
    D3D11_BUFFER_DESC bd = {0};
    bd.ByteWidth = sizeof(Vertex) * 6;
    bd.Usage = D3D11_USAGE_DYNAMIC;
    bd.BindFlags = D3D11_BIND_VERTEX_BUFFER;
    bd.CPUAccessFlags = D3D11_CPU_ACCESS_WRITE;
    r->device->CreateBuffer(&bd, NULL, &r->vertexBuf);

    return 0;
}

/* ─── Debug logging ─────────────────────────────────────────────────────── */

static FILE* g_logFile = NULL;

static void logMsg(const char* fmt, ...) {
    if (!g_logFile) {
        char logPath[MAX_PATH];
        GetTempPathA(MAX_PATH, logPath);
        strcat_s(logPath, MAX_PATH, "d3d11render_debug.log");
        g_logFile = fopen(logPath, "a");
    }
    if (g_logFile) {
        va_list args;
        va_start(args, fmt);
        vfprintf(g_logFile, fmt, args);
        va_end(args);
        fflush(g_logFile);
    }
}

/* ─── SEH wrapper for D3D11CreateDevice ─────────────────────────────────── */

/* __try/__except cannot coexist with C++ objects in the same function,
   so we isolate the call in a plain-C helper. Returns hr; on SEH exception
   sets *sehCode and returns E_FAIL. */
static HRESULT seh_D3D11CreateDevice(
    IDXGIAdapter* pAdapter, D3D_DRIVER_TYPE DriverType, HMODULE Software,
    UINT Flags, const D3D_FEATURE_LEVEL* pFeatureLevels, UINT FeatureLevels,
    UINT SDKVersion, ID3D11Device** ppDevice, D3D_FEATURE_LEVEL* pFeatureLevel,
    ID3D11DeviceContext** ppImmediateContext, DWORD* sehCode)
{
    *sehCode = 0;
    __try {
        return D3D11CreateDevice(pAdapter, DriverType, Software, Flags,
                                 pFeatureLevels, FeatureLevels, SDKVersion,
                                 ppDevice, pFeatureLevel, ppImmediateContext);
    } __except ((*sehCode = GetExceptionCode()), EXCEPTION_EXECUTE_HANDLER) {
        return E_FAIL;
    }
}

/* ─── Create / Destroy ────────────────────────────────────────────────────── */

R_API void* D3D11R_CreateWithDevice(void* hWnd, void* existingDevice);
R_API void* D3D11R_GetDevice(void* handle);

R_API void* D3D11R_Create(void* hWnd) {
    return D3D11R_CreateWithDevice(hWnd, NULL);
}

/* GetDevice 返回渲染器的 ID3D11Device（AddRef），供 FFmpeg 解码器复用以实现同设备零拷贝。 */
R_API void* D3D11R_GetDevice(void* handle) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->device) return NULL;
    r->device->AddRef();
    return r->device;
}

/* IsWarp 返回 1 表示当前 device 是 WARP 软光栅（无 GPU 回退）。
   调用方据此判断是否把 device 传给解码器做 D3D11VA 硬解：
   WARP device 无视频解码器，传给它会导致硬解失败/崩溃，应让解码器纯软解。 */
R_API int D3D11R_IsWarp(void* handle) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r) return 0;
    return r->isWarp;
}

/* CanRenderNV12 返回 1 表示渲染器具备 NV12 平面 SRV 能力（D3D11.3 PlaneSlice）。
   0 时解码器应输出 BGRA（transfer_data 回读），否则 GPU 模式将黑屏。 */
R_API int D3D11R_CanRenderNV12(void* handle) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r) return 0;
    return (r->device3 != NULL) ? 1 : 0;
}

/* Create renderer using an existing D3D11 device (for zero-copy GPU texture sharing).
   If device is NULL, creates its own device (fallback for BGRA CPU path). */
R_API void* D3D11R_CreateWithDevice(void* hWnd, void* existingDevice) {
    D3D11Renderer* r = (D3D11Renderer*)calloc(1, sizeof(D3D11Renderer));
    if (!r) return NULL;

    InitializeCriticalSection(&r->cs);
    r->hWnd = (HWND)hWnd;
    r->lbStale = 1;
    r->copySlot = -1;
    r->drawnSlot = -1;

    RECT rc;
    GetClientRect(r->hWnd, &rc);
    r->width = (int)(rc.right - rc.left);
    r->height = (int)(rc.bottom - rc.top);

    logMsg("[D3D11R_CreateWithDevice] hWnd=%p existingDevice=%p size=%dx%d\n",
           hWnd, existingDevice, r->width, r->height);

    if (existingDevice) {
        r->device = (ID3D11Device*)existingDevice;
        r->device->AddRef();
        r->ctx = NULL;
        r->device->GetImmediateContext(&r->ctx);

        ID3D11Multithread* mt = NULL;
        if (SUCCEEDED(r->ctx->QueryInterface(__uuidof(ID3D11Multithread), (void**)&mt))) {
            mt->SetMultithreadProtected(TRUE);
            mt->Release();
        }
    } else {
        static const UINT flags = D3D11_CREATE_DEVICE_BGRA_SUPPORT;
        static const D3D_FEATURE_LEVEL tryLevels[] = {
            D3D_FEATURE_LEVEL_11_1,
            D3D_FEATURE_LEVEL_11_0,
            D3D_FEATURE_LEVEL_10_1,
            D3D_FEATURE_LEVEL_10_0,
            D3D_FEATURE_LEVEL_9_3,
        };
        D3D_FEATURE_LEVEL fl;
        HRESULT hr = E_FAIL;
        DWORD sehCode = 0;

        for (int i = 0; i < (int)(sizeof(tryLevels)/sizeof(tryLevels[0])); i++) {
            hr = seh_D3D11CreateDevice(NULL, D3D_DRIVER_TYPE_HARDWARE, NULL, flags,
                                       &tryLevels[i], 1, D3D11_SDK_VERSION,
                                       &r->device, &fl, &r->ctx, &sehCode);
            if (sehCode) {
                logMsg("[D3D11R_CreateWithDevice] HARDWARE SEH exception 0x%08X\n", (unsigned)sehCode);
                break;
            }
            logMsg("[D3D11R_CreateWithDevice] HARDWARE hr=0x%08X fl=%d_%d\n",
                   (unsigned)hr, fl >> 12, (fl >> 8) & 0xF);
            if (SUCCEEDED(hr)) break;
        }

        if (FAILED(hr) && !sehCode) {
            logMsg("[D3D11R_CreateWithDevice] HARDWARE failed, trying WARP...\n");
            for (int i = 0; i < (int)(sizeof(tryLevels)/sizeof(tryLevels[0])); i++) {
                hr = seh_D3D11CreateDevice(NULL, D3D_DRIVER_TYPE_WARP, NULL, flags,
                                           &tryLevels[i], 1, D3D11_SDK_VERSION,
                                           &r->device, &fl, &r->ctx, &sehCode);
                if (sehCode) {
                    logMsg("[D3D11R_CreateWithDevice] WARP SEH exception 0x%08X\n", (unsigned)sehCode);
                    break;
                }
                logMsg("[D3D11R_CreateWithDevice] WARP hr=0x%08X fl=%d_%d\n",
                       (unsigned)hr, fl >> 12, (fl >> 8) & 0xF);
                if (SUCCEEDED(hr)) { r->isWarp = 1; break; }
            }
        }

        if (FAILED(hr)) { DeleteCriticalSection(&r->cs); free(r); return NULL; }

        /* 自建设备也要启用多线程保护（解码线程 + 渲染线程共用 device） */
        ID3D11Multithread* mt2 = NULL;
        if (SUCCEEDED(r->ctx->QueryInterface(__uuidof(ID3D11Multithread), (void**)&mt2))) {
            mt2->SetMultithreadProtected(TRUE);
            mt2->Release();
        }
    }

    logMsg("[D3D11R_CreateWithDevice] calling initRenderer...\n");
    if (initRenderer(r) != 0) {
        logMsg("[D3D11R_CreateWithDevice] initRenderer FAILED\n");
        r->device->Release();
        DeleteCriticalSection(&r->cs);
        free(r);
        return NULL;
    }

    /* Clear back buffer to black immediately so no uninitialized (green) content
       is ever presented before the first real frame arrives.
       FLIP_DISCARD + BufferCount=2: double-buffered. Present(1,0) enables VSync.
       After Present, back buffer is undefined — each frame's ClearRenderTargetView
       before draw handles this correctly (mpv/VLC use this mode). */
    if (r->rtv && r->ctx) {
        float black[4] = {0, 0, 0, 1};
        r->ctx->ClearRenderTargetView(r->rtv, black);
        HRESULT pr = r->swapChain->Present(1, 0);
        if (FAILED(pr)) {
            logMsg("[D3D11R_CreateWithDevice] initial Present failed: hr=0x%08X\n", (unsigned)pr);
        }
    }

    logMsg("[D3D11R_CreateWithDevice] success\n");
    /* Query ID3D11Device3 for NV12 plane SRV1（PlaneSlice，D3D11.3+） */
    r->device3 = NULL;
    r->device->QueryInterface(__uuidof(ID3D11Device3), (void**)&r->device3);
    logMsg("[D3D11R_CreateWithDevice] device3=0x%p\n", (void*)r->device3);
    /* Query ID3D11DeviceContext1 for CopySubresourceRegion1（COPY_DISCARD 防闪屏） */
    r->ctx1 = NULL;
    r->ctx->QueryInterface(__uuidof(ID3D11DeviceContext1), (void**)&r->ctx1);
    return r;
}

R_API void D3D11R_Destroy(void* handle) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r) return;
    /* 获取临界区再释放资源，与 D3D11R_Present 的 TryEnterCriticalSection 互斥，
       避免 Destroy free(r) 后 Present 还在访问 r 的 USE AFTER FREE。
       Destroy 用 Enter（可等），Present 用 TryEnter（不可等，跳过本帧），
       保证 Destroy 不会在 Present 中途释放资源。 */
    EnterCriticalSection(&r->cs);
    for (int i = 0; i < 2; i++) {
        if (r->srvCopyY[i]) r->srvCopyY[i]->Release();
        if (r->srvCopyUV[i]) r->srvCopyUV[i]->Release();
        if (r->copyTex[i]) r->copyTex[i]->Release();
    }
    if (r->srvBgra) r->srvBgra->Release();
    if (r->bgraTex) r->bgraTex->Release();
    if (r->stagingTex) r->stagingTex->Release();
    if (r->uvProbeTex) r->uvProbeTex->Release();
    if (r->lumaProbeTex) r->lumaProbeTex->Release();
    if (r->vertexBuf) r->vertexBuf->Release();
    if (r->sampler) r->sampler->Release();
    if (r->inputLayout) r->inputLayout->Release();
    if (r->vs) r->vs->Release();
    if (r->ps) r->ps->Release();
    if (r->nv12ps) r->nv12ps->Release();
    if (r->rtv) r->rtv->Release();
    if (r->swapChain) r->swapChain->Release();
    if (r->ctx1) r->ctx1->Release();
    if (r->ctx) r->ctx->Release();
    if (r->device3) r->device3->Release();
    if (r->device) r->device->Release();
    LeaveCriticalSection(&r->cs);
    DeleteCriticalSection(&r->cs);
    free(r);
}

/* ─── Resize ──────────────────────────────────────────────────────────────── */

/* Internal: render BGRA data without taking the lock (caller must hold cs) */
static int renderBGRAInternal(D3D11Renderer* r, const void* bgraData, int frameW, int frameH, int stride) {
    if (!r->rtv) return -1;

    if (r->srcW != frameW || r->srcH != frameH) {
        r->srcW = frameW;
        r->srcH = frameH;
        r->lbStale = 1;
    }

    ID3D11Texture2D* tex = getBgraTexture(r, frameW, frameH);
    if (!tex || !r->srvBgra) return -1;

    D3D11_MAPPED_SUBRESOURCE ms;
    HRESULT hr = r->ctx->Map(tex, 0, D3D11_MAP_WRITE_DISCARD, 0, &ms);
    if (FAILED(hr)) return -1;

    const uint8_t* src = (const uint8_t*)bgraData;
    uint8_t* dst = (uint8_t*)ms.pData;
    UINT srcPitch = (stride > 0) ? (UINT)stride : (UINT)(frameW * 4);
    /* WARP（软光栅）下 WRITE_DISCARD 可能不彻底清旧帧，整体残留。
       先清整张 mapped region（含 padding 区），再逐行写入，确保无旧帧残留。 */
    if (r->isWarp) {
        memset(dst, 0, (size_t)ms.RowPitch * frameH);
    }
    for (int y = 0; y < frameH; y++) {
        memcpy(dst, src, (size_t)(frameW * 4));
        src += srcPitch;
        dst += ms.RowPitch;
    }
    r->ctx->Unmap(tex, 0);

    updateVertices(r);

    float black[4] = {0, 0, 0, 1};
    r->ctx->ClearRenderTargetView(r->rtv, black);
    r->ctx->OMSetRenderTargets(1, &r->rtv, NULL);
    D3D11_VIEWPORT vp = {0, 0, (FLOAT)r->width, (FLOAT)r->height, 0, 1};
    r->ctx->RSSetViewports(1, &vp);

    r->ctx->IASetInputLayout(r->inputLayout);
    r->ctx->VSSetShader(r->vs, NULL, 0);
    r->ctx->PSSetShader(r->ps, NULL, 0);
    r->ctx->PSSetShaderResources(0, 1, &r->srvBgra);
    r->ctx->PSSetSamplers(0, 1, &r->sampler);

    UINT vstride = sizeof(Vertex), offset = 0;
    r->ctx->IASetVertexBuffers(0, 1, &r->vertexBuf, &vstride, &offset);
    r->ctx->IASetPrimitiveTopology(D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
    r->ctx->Draw(6, 0);

    InterlockedExchange(&r->pendingFrame, 1);
    InterlockedExchange(&r->hasEverRendered, 1);
    r->lastDrawTick = GetTickCount64();
    return 0;
}

R_API void D3D11R_Resize(void* handle, int w, int h) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->swapChain) return;

    EnterCriticalSection(&r->cs);
    logMsg("[DLL Resize] requested=%dx%d current=%dx%d", w, h, r->width, r->height);
    if (r->width == w && r->height == h) {
        /* Even if size unchanged, mark letterbox stale so computeLetterbox
           re-evaluates with current HWND dimensions on next render. */
        r->lbStale = 1;
        LeaveCriticalSection(&r->cs);
        return;
    }

    /* Save last frame info before resize invalidates everything */
    ID3D11Texture2D* lastTex = r->bgraTex;
    int lastTexW = r->bgraTexW, lastTexH = r->bgraTexH;
    int hadFrame = (lastTex != NULL || r->drawnSlot >= 0) ? 1 : 0;

    if (r->rtv) { r->rtv->Release(); r->rtv = NULL; }
    r->ctx->OMSetRenderTargets(0, NULL, NULL);

    HRESULT hr = r->swapChain->ResizeBuffers(0, (UINT)w, (UINT)h, DXGI_FORMAT_B8G8R8A8_UNORM, 0);
    if (FAILED(hr)) {
        logMsg("[DLL Resize] ResizeBuffers failed: hr=0x%08X\n", (unsigned)hr);
        LeaveCriticalSection(&r->cs);
        return;
    }
    r->width = w;
    r->height = h;
    r->lbStale = 1;

    ID3D11Texture2D* backBuf = NULL;
    r->swapChain->GetBuffer(0, __uuidof(ID3D11Texture2D), (void**)&backBuf);
    if (backBuf) {
        r->device->CreateRenderTargetView(backBuf, NULL, &r->rtv);
        backBuf->Release();
    }

    /* Always clear the new back buffer to black FIRST, so uninitialized GPU memory
       (typically green) is never presented even if redraw fails. */
    int redrawOk = 0;
    if (hadFrame && r->rtv && r->srcW > 0 && r->srcH > 0) {
        float black[4] = {0, 0, 0, 1};
        r->ctx->ClearRenderTargetView(r->rtv, black);

        if (r->drawnSlot >= 0 && r->srvCopyY[r->drawnSlot] && r->srvCopyUV[r->drawnSlot]) {
            /* NV12 槽位路径：用缓存的 D3D11.3 双 SRV（PlaneSlice）重绘最近一帧 */
            updateVertices(r);
            r->ctx->OMSetRenderTargets(1, &r->rtv, NULL);
            D3D11_VIEWPORT vp = {0, 0, (FLOAT)r->width, (FLOAT)r->height, 0, 1};
            r->ctx->RSSetViewports(1, &vp);
            r->ctx->IASetInputLayout(r->inputLayout);
            r->ctx->VSSetShader(r->vs, NULL, 0);
            r->ctx->PSSetShader(r->nv12ps, NULL, 0);
            ID3D11ShaderResourceView* srvs[2] = { r->srvCopyY[r->drawnSlot], r->srvCopyUV[r->drawnSlot] };
            r->ctx->PSSetShaderResources(0, 2, srvs);
            r->ctx->PSSetSamplers(0, 1, &r->sampler);
            UINT vstride = sizeof(Vertex), offset = 0;
            r->ctx->IASetVertexBuffers(0, 1, &r->vertexBuf, &vstride, &offset);
            r->ctx->IASetPrimitiveTopology(D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
            r->ctx->Draw(6, 0);
            redrawOk = 1;
        } else if (lastTex && lastTexW > 0 && lastTexH > 0 && r->srvBgra) {
            /* BGRA CPU path: redraw from cached upload texture */
            updateVertices(r);
            r->ctx->OMSetRenderTargets(1, &r->rtv, NULL);
            D3D11_VIEWPORT vp = {0, 0, (FLOAT)r->width, (FLOAT)r->height, 0, 1};
            r->ctx->RSSetViewports(1, &vp);
            r->ctx->IASetInputLayout(r->inputLayout);
            r->ctx->VSSetShader(r->vs, NULL, 0);
            r->ctx->PSSetShader(r->ps, NULL, 0);
            r->ctx->PSSetShaderResources(0, 1, &r->srvBgra);
            r->ctx->PSSetSamplers(0, 1, &r->sampler);
            UINT vstride = sizeof(Vertex), offset = 0;
            r->ctx->IASetVertexBuffers(0, 1, &r->vertexBuf, &vstride, &offset);
            r->ctx->IASetPrimitiveTopology(D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
            r->ctx->Draw(6, 0);
            redrawOk = 1;
        }
    } else if (r->rtv && r->ctx) {
        /* No frame has ever been rendered — clear to black. */
        float black[4] = {0, 0, 0, 1};
        r->ctx->ClearRenderTargetView(r->rtv, black);
    }

    if (redrawOk) {
        InterlockedExchange(&r->pendingFrame, 1);
    } else if (r->rtv && r->ctx) {
        /* Redraw failed or no frame to redraw — present the black buffer immediately
           so no uninitialized (green) content is ever shown. */
        HRESULT pr = r->swapChain->Present(1, 0);
        if (FAILED(pr)) {
            logMsg("[DLL Resize] Present(black) failed: hr=0x%08X\n", (unsigned)pr);
        }
    }

    LeaveCriticalSection(&r->cs);
}

/* ─── NV12 copyTex 槽位管理 ───────────────────────────────────────────────── */

/* ensureCopySlot 创建/重建指定槽位的 NV12 copyTex + 双 SRV（D3D11.3 PlaneSlice）
   尺寸检查按槽位独立进行（转屏时两个槽位分别重建，互不干扰）。 */
static int ensureCopySlot(D3D11Renderer* r, int slot, int frameW, int frameH) {
    if (slot < 0 || slot > 1) return -1;

    if (r->copyTex[slot] && r->copyTexW[slot] == frameW && r->copyTexH[slot] == frameH &&
        r->srvCopyY[slot] && r->srvCopyUV[slot]) {
        return 0;
    }

    if (r->copyTex[slot]) { r->copyTex[slot]->Release(); r->copyTex[slot] = NULL; }
    if (r->srvCopyY[slot]) { r->srvCopyY[slot]->Release(); r->srvCopyY[slot] = NULL; }
    if (r->srvCopyUV[slot]) { r->srvCopyUV[slot]->Release(); r->srvCopyUV[slot] = NULL; }

    D3D11_TEXTURE2D_DESC copyDesc = {0};
    copyDesc.Width = (UINT)frameW;
    copyDesc.Height = (UINT)frameH;
    copyDesc.MipLevels = 1;
    copyDesc.ArraySize = 1;
    copyDesc.Format = DXGI_FORMAT_NV12;
    copyDesc.SampleDesc.Count = 1;
    copyDesc.Usage = D3D11_USAGE_DEFAULT;
    copyDesc.BindFlags = D3D11_BIND_SHADER_RESOURCE;
    HRESULT hr = r->device->CreateTexture2D(&copyDesc, NULL, &r->copyTex[slot]);
    if (FAILED(hr) || !r->copyTex[slot]) {
        logMsg("[ensureCopySlot] CreateTexture2D(NV12) slot=%d failed: hr=0x%08X\n", slot, (unsigned)hr);
        return -1;
    }

    if (r->device3) {
        D3D11_SHADER_RESOURCE_VIEW_DESC1 sd;
        ZeroMemory(&sd, sizeof(sd));
        sd.ViewDimension = D3D11_SRV_DIMENSION_TEXTURE2DARRAY;
        sd.Texture2DArray.MipLevels = 1;
        sd.Texture2DArray.ArraySize = 1;
        sd.Format = DXGI_FORMAT_R8_UNORM; sd.Texture2DArray.PlaneSlice = 0;
        ID3D11ShaderResourceView1* y1 = NULL;
        r->device3->CreateShaderResourceView1(r->copyTex[slot], &sd, &y1);
        r->srvCopyY[slot] = (ID3D11ShaderResourceView*)y1;
        sd.Format = DXGI_FORMAT_R8G8_UNORM; sd.Texture2DArray.PlaneSlice = 1;
        ID3D11ShaderResourceView1* uv1 = NULL;
        r->device3->CreateShaderResourceView1(r->copyTex[slot], &sd, &uv1);
        r->srvCopyUV[slot] = (ID3D11ShaderResourceView*)uv1;
    }

    r->copyTexW[slot] = frameW;
    r->copyTexH[slot] = frameH;

    if (!r->srvCopyY[slot] || !r->srvCopyUV[slot]) {
        logMsg("[ensureCopySlot] SRV create failed slot=%d device3=%p\n", slot, (void*)r->device3);
        return -1;
    }
    return 0;
}

/* drawSlotInternal 绘制指定槽位（调用方必须持有 cs）。 */
static int drawSlotInternal(D3D11Renderer* r, int slot, int frameW, int frameH) {
    if (!r->rtv || !r->ctx) return -1;
    if (slot < 0 || slot > 1) return -1;
    if (!r->srvCopyY[slot] || !r->srvCopyUV[slot]) return -1;

    if (r->srcW != frameW || r->srcH != frameH) {
        r->srcW = frameW;
        r->srcH = frameH;
        r->lbStale = 1;
    }

    updateVertices(r);

    float black[4] = {0, 0, 0, 1};
    r->ctx->ClearRenderTargetView(r->rtv, black);
    r->ctx->OMSetRenderTargets(1, &r->rtv, NULL);
    D3D11_VIEWPORT vp = {0, 0, (FLOAT)r->width, (FLOAT)r->height, 0, 1};
    r->ctx->RSSetViewports(1, &vp);

    r->ctx->IASetInputLayout(r->inputLayout);
    r->ctx->VSSetShader(r->vs, NULL, 0);
    r->ctx->PSSetShader(r->nv12ps, NULL, 0);
    /* t0=当前帧 Y, t1=当前帧 UV。shader 逐像素 UV<0.01 视为中性 128（D3D11VA
       偶发半帧 → 灰度显示），零 CPU Map 回读，GPU 硬解优势释放。 */
    ID3D11ShaderResourceView* srvs[2] = { r->srvCopyY[slot], r->srvCopyUV[slot] };
    r->ctx->PSSetShaderResources(0, 2, srvs);
    r->ctx->PSSetSamplers(0, 1, &r->sampler);

    UINT vstride = sizeof(Vertex), offset = 0;
    r->ctx->IASetVertexBuffers(0, 1, &r->vertexBuf, &vstride, &offset);
    r->ctx->IASetPrimitiveTopology(D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
    r->ctx->Draw(6, 0);

    r->drawnSlot = slot;
    InterlockedExchange(&r->pendingFrame, 1);
    InterlockedExchange(&r->hasEverRendered, 1);
    r->lastDrawTick = GetTickCount64();
    return 0;
}

/* ─── GPU 纹理路径（双槽 copyTex，mpv copy_tex 模式） ─────────────────────── */

/* D3D11R_CopyTexture：解码线程调用。把 FFmpeg hwframe 纹理拷入本渲染器下一个
   copyTex 槽位并返回槽号。拷贝命令提交到 immediate context 后调用方即可释放源帧
   （同一 context 内 GPU 命令按提交序串行执行，后续解码写入必然在本次拷贝完成后）。
   返回 0 成功（*outSlot=槽号），-1 失败。 */
R_API int D3D11R_CopyTexture(void* handle, void* texture, int subIdx, int frameW, int frameH, int* outSlot) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->device || !r->ctx || !texture || !outSlot) return -1;

    EnterCriticalSection(&r->cs);

    /* 设备一致性检查：texture 来自 FFmpeg 解码器设备，渲染器必须用同一设备 */
    ID3D11Device* texDevice = NULL;
    ((ID3D11Texture2D*)texture)->GetDevice(&texDevice);
    if (texDevice != r->device) {
        if (texDevice) texDevice->Release();
        logMsg("[CopyTexture] device mismatch: tex=%p renderer=%p\n", (void*)texDevice, (void*)r->device);
        LeaveCriticalSection(&r->cs);
        return -1;
    }
    if (texDevice) texDevice->Release();

    /* 双槽轮换：下一次写入与最近绘制的槽位错开，避免与 UI 线程 Draw 竞争同一纹理 */
    int slot = (r->copySlot + 1) % 2;
    if (ensureCopySlot(r, slot, frameW, frameH) != 0) {
        LeaveCriticalSection(&r->cs);
        return -1;
    }

    UINT srcSub = D3D11CalcSubresource(0, (UINT)subIdx, 1);
    /* box 必须覆盖 Y+UV 两平面：NV12 纹理的 UV plane 位于 y≥frameH 区域，
       源 hwframe 同布局。只拷 Y（height=frameH）时 copyTex 的 UV plane 永远
       保持创建初值全 0 → UvProbeSlot 每帧误判 UV=0 坏帧（streak 无限涨、
       画面全程灰度降级）且 LumaProbe 被 probe==1 短路（黑帧检测整体失效）。 */
    D3D11_BOX box = {0, 0, 0, (UINT)frameW, (UINT)(frameH + frameH / 2), 1};
    /* CopySubresourceRegion1 + D3D11_COPY_DISCARD：告知 GPU 旧 copyTex 内容可丢弃，
       无需等待上一帧 Draw 完成即可覆盖（mpv 做法）。 */
    if (r->ctx1) {
        r->ctx1->CopySubresourceRegion1(r->copyTex[slot], 0, 0, 0, 0,
                                         (ID3D11Resource*)texture, srcSub, &box, D3D11_COPY_DISCARD);
    } else {
        r->ctx->CopySubresourceRegion(r->copyTex[slot], 0, 0, 0, 0,
                                      (ID3D11Resource*)texture, srcSub, &box);
    }

    r->copySlot = slot;
    *outSlot = slot;
    LeaveCriticalSection(&r->cs);
    return 0;
}

/* ensureStaging 创建/重建纹理回读 staging（NV12，CPU_ACCESS_READ，BindFlags=0，
   mpv ra_d3d11.c:492-505 同款）。分辨率变化时重建。调用方必须持有 cs。 */
static int ensureStaging(D3D11Renderer* r, int frameW, int frameH) {
    if (r->stagingTex && r->stagingW == frameW && r->stagingH == frameH) return 0;
    if (r->stagingTex) { r->stagingTex->Release(); r->stagingTex = NULL; }
    D3D11_TEXTURE2D_DESC desc = {0};
    desc.Width = (UINT)frameW;
    desc.Height = (UINT)frameH;
    desc.MipLevels = 1;
    desc.ArraySize = 1;
    desc.Format = DXGI_FORMAT_NV12;
    desc.SampleDesc.Count = 1;
    desc.Usage = D3D11_USAGE_STAGING;
    desc.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
    desc.BindFlags = 0;
    HRESULT hr = r->device->CreateTexture2D(&desc, NULL, &r->stagingTex);
    if (FAILED(hr) || !r->stagingTex) {
        logMsg("[ensureStaging] CreateTexture2D(NV12 STAGING) %dx%d failed: hr=0x%08X\n", frameW, frameH, (unsigned)hr);
        return -1;
    }
    r->stagingW = frameW;
    r->stagingH = frameH;
    return 0;
}

/* D3D11R_ReadSlot 回读指定 copyTex 槽位的 NV12 帧（mpv ra_d3d11.c:736-767 同款）：
   CopyResource(staging, copyTex[slot]) → Map(READ) → 按 RowPitch 逐行拷 Y/UV 两平面
   到 outBuf（紧凑 packing：Y=W*H，UV=W*H/2，outRowPitch=W）→ Unmap。
   调用时机不在 Present/解码线程（控制 server 的 HTTP goroutine），cs 与渲染互斥。
   outBuf 由调用方分配，容量 ≥ frameW*frameH*3/2。返回 0 成功，-1 失败。 */
R_API int D3D11R_ReadSlot(void* handle, int slot, int frameW, int frameH,
                          uint8_t* outBuf, int outBufSize,
                          int* outW, int* outH, int* outRowPitch) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->device || !r->ctx) return -1;
    if (slot < 0 || slot > 1 || frameW <= 0 || frameH <= 0 || !outBuf) return -1;
    if (outBufSize < frameW * frameH * 3 / 2) return -1;

    EnterCriticalSection(&r->cs);
    if (!r->copyTex[slot] || r->copyTexW[slot] != frameW || r->copyTexH[slot] != frameH) {
        LeaveCriticalSection(&r->cs);
        return -1;   /* 槽位尚未拷入或分辨率已变 */
    }
    if (ensureStaging(r, frameW, frameH) != 0) {
        LeaveCriticalSection(&r->cs);
        return -1;
    }
    r->ctx->CopyResource((ID3D11Resource*)r->stagingTex, (ID3D11Resource*)r->copyTex[slot]);
    D3D11_MAPPED_SUBRESOURCE mapped;
    ZeroMemory(&mapped, sizeof(mapped));
    HRESULT hr = r->ctx->Map((ID3D11Resource*)r->stagingTex, 0, D3D11_MAP_READ, 0, &mapped);
    if (FAILED(hr) || !mapped.pData) {
        LeaveCriticalSection(&r->cs);
        logMsg("[ReadSlot] Map failed: hr=0x%08X\n", (unsigned)hr);
        return -1;
    }
    /* 按 RowPitch 逐行拷 Y 平面（H 行）与 UV 平面（H/2 行，紧随 Y），紧凑 packing */
    uint8_t* dst = outBuf;
    const uint8_t* src = (const uint8_t*)mapped.pData;
    for (int y = 0; y < frameH; y++) {
        memcpy(dst, src + (SIZE_T)mapped.RowPitch * y, frameW);
        dst += frameW;
    }
    const uint8_t* uvSrc = src + (SIZE_T)mapped.RowPitch * frameH;
    for (int y = 0; y < frameH / 2; y++) {
        memcpy(dst, uvSrc + (SIZE_T)mapped.RowPitch * y, frameW);
        dst += frameW;
    }
    r->ctx->Unmap((ID3D11Resource*)r->stagingTex, 0);
    if (outW) *outW = frameW;
    if (outH) *outH = frameH;
    if (outRowPitch) *outRowPitch = frameW;
    LeaveCriticalSection(&r->cs);
    return 0;
}

/* uvProbeStaging 惰性创建 64x4 NV12 STAGING 探针纹理（调用方必须持有 cs）。 */
static int uvProbeStaging(D3D11Renderer* r) {
    if (r->uvProbeTex) return 0;
    D3D11_TEXTURE2D_DESC desc;
    ZeroMemory(&desc, sizeof(desc));
    desc.Width = 64;
    desc.Height = 4;
    desc.MipLevels = 1;
    desc.ArraySize = 1;
    desc.Format = DXGI_FORMAT_NV12;
    desc.SampleDesc.Count = 1;
    desc.Usage = D3D11_USAGE_STAGING;
    desc.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
    desc.BindFlags = 0;
    HRESULT hr = r->device->CreateTexture2D(&desc, NULL, &r->uvProbeTex);
    if (FAILED(hr) || !r->uvProbeTex) {
        logMsg("[uvProbe] CreateTexture2D(NV12 64x4 STAGING) failed: hr=0x%08X\
", (unsigned)hr);
        return -1;
    }
    return 0;
}

/* D3D11R_UvProbeSlot：对指定 copyTex 槽位的 UV plane 顶部区域（64x4，覆盖 64x8
   像素的色度）做 STAGING 回读采样，判断 UV 是否全 0（D3D11VA 半帧/损坏帧，
   limited-range NV12 非法值，零误判）。解码线程在 CopyTexture 之后调用，
   cs 与 Present/渲染互斥，无共享 context 竞态。
   采样区域微小（64x4 = 384 字节），GPU CopySubresource + Map stall 可忽略。
   返回 1=UV 全 0（坏帧），0=正常，-1=探针失败（调用方按正常帧放行，不误伤）。
   frameW/frameH 必须与槽位尺寸一致（与 ReadSlot 同样的槽位校验）。 */
R_API int D3D11R_UvProbeSlot(void* handle, int slot, int frameW, int frameH) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->device || !r->ctx) return -1;
    if (slot < 0 || slot > 1 || frameW <= 0 || frameH <= 0) return -1;

    EnterCriticalSection(&r->cs);
    if (!r->copyTex[slot] || r->copyTexW[slot] != frameW || r->copyTexH[slot] != frameH) {
        LeaveCriticalSection(&r->cs);
        return -1;   /* 槽位尚未拷入或分辨率已变 */
    }
    if (uvProbeStaging(r) != 0) {
        LeaveCriticalSection(&r->cs);
        return -1;
    }
    /* 拷贝形态（uvmin4/uvmin5 实验定型）：src box 从 y=0 横跨 Y+UV plane
       （高 frameH+2，即 Y 全部 + UV 顶 2 行），dst(0,0)。
       驱动对横跨 box 做平面感知拆分：dst Y 行收 src Y 行、dst UV 行收 src UV 行
       （行梯度逐值验证）。注意 dst 点 y 必须 < dst 纹理 Height（Y plane 内），
       直接以 y=frameH 为 dst 偏移或 sub=1 寻址 UV plane 都会被静默 noop
       （uvmin3 实验：D/E/F 全 0），这是旧版探针恒阳性的根因。
       探针纹理 64x4 NV12：Y=0..3 行，UV=4..5 行（=源 UV 顶 2 行，64x4 像素色度）。 */
    D3D11_BOX box = { 0, 0, 0, 64, (UINT)(frameH + 2), 1 };
    r->ctx->CopySubresourceRegion(r->uvProbeTex, 0, 0, 0, 0,
                                  (ID3D11Resource*)r->copyTex[slot], 0, &box);
    D3D11_MAPPED_SUBRESOURCE mapped;
    ZeroMemory(&mapped, sizeof(mapped));
    HRESULT hr = r->ctx->Map((ID3D11Resource*)r->uvProbeTex, 0, D3D11_MAP_READ, 0, &mapped);
    if (FAILED(hr) || !mapped.pData) {
        LeaveCriticalSection(&r->cs);
        logMsg("[uvProbe] Map failed: hr=0x%08X\
", (unsigned)hr);
        return -1;
    }
    int allZero = 1;
    const uint8_t* row = (const uint8_t*)mapped.pData;
    for (int y = 4; y < 6 && allZero; y++) {
        for (int x = 0; x < 64; x++) {
            if (row[(SIZE_T)y * mapped.RowPitch + x] != 0) { allZero = 0; break; }
        }
    }
    r->ctx->Unmap((ID3D11Resource*)r->uvProbeTex, 0);
    LeaveCriticalSection(&r->cs);
    return allZero;
}

/* LUMA_PROBE_BANDS 上/中/下三个水平采样条带的起始行号（按帧高比例定位，
   运行时换算）。单条带 4 行可能采到「下半屏黑、上半屏正常」的漏检死角，
   3 条带覆盖上中下后该形态必然命中。 */
#define LUMA_PROBE_BANDS 3

/* lumaProbeStaging 惰性创建 64x12 NV12 STAGING 亮度探针纹理（调用方必须持有 cs）。 */
static int lumaProbeStaging(D3D11Renderer* r) {
    if (r->lumaProbeTex) return 0;
    D3D11_TEXTURE2D_DESC desc;
    ZeroMemory(&desc, sizeof(desc));
    desc.Width = 64;
    desc.Height = 12;  /* Y 12 行（3 条带 x4 行，y=0..11）+ UV chroma 前 2 行（y=12..13） */
    desc.MipLevels = 1;
    desc.ArraySize = 1;
    desc.Format = DXGI_FORMAT_NV12;
    desc.SampleDesc.Count = 1;
    desc.Usage = D3D11_USAGE_STAGING;
    desc.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
    desc.BindFlags = 0;
    HRESULT hr = r->device->CreateTexture2D(&desc, NULL, &r->lumaProbeTex);
    if (FAILED(hr) || !r->lumaProbeTex) {
        logMsg("[lumaProbe] CreateTexture2D(NV12 64x12 STAGING) failed: hr=0x%08X\n", (unsigned)hr);
        return -1;
    }
    return 0;
}

/* D3D11R_LumaProbeSlot：对指定 copyTex 槽位做亮度+色度联合采样（staging 回读），
   黑帧检测与坏帧分类用（解码线程在 CopyTexture 之后调用，cs 与 Present 互斥）。
   采样：Y plane 上/中/下 3 条带各 64x4（亮度）+ UV plane 顶部 64x4（色度）。
   返回值（位掩码）：
     bit0(Y-dark)=1 : Y 采样暗点（<16）占比 ≥95%（limited-range 近纯黑；
                      比例判定容忍「90% 黑带少量内容」的脏帧，不再全量判定）
     bit1(UV-zero)=1: UV 采样全 0（未初始化/半帧，与 UvProbeSlot 同判）
     bit2(UV-flat)=1: UV 非 0 但全 128（黑色画面配合的"标准黑"色度）
     全 0 = 正常帧（有亮度内容）
     -1 = 探针失败（调用方按正常帧放行，不误伤）
   Y 暗占比 ≥95% + UV 全 0/128 同时命中 → 高置信黑帧；
   Y 暗占比 ≥95% 但 UV 有杂色 → 编码器异常黑帧（色度残留），bit0 仍置位。 */
R_API int D3D11R_LumaProbeSlot(void* handle, int slot, int frameW, int frameH) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->device || !r->ctx) return -1;
    if (slot < 0 || slot > 1 || frameW <= 0 || frameH <= 0) return -1;

    EnterCriticalSection(&r->cs);
    if (!r->copyTex[slot] || r->copyTexW[slot] != frameW || r->copyTexH[slot] != frameH) {
        LeaveCriticalSection(&r->cs);
        return -1;   /* 槽位尚未拷入或分辨率已变 */
    }
    if (lumaProbeStaging(r) != 0) {
        LeaveCriticalSection(&r->cs);
        return -1;
    }
    /* 拷贝顺序（必须先 UV 后条带，不可颠倒）：UV 采样用横跨 box（src
       y=0..frameH+2）+ dst(0,0)，驱动平面感知拆分会把 dst 的全部 12 行 Y
       （探针纹理 y=0..11）+ dst UV plane（y=12..13，收 src UV 顶 2 行）一并写入
       —— 语义同 UvProbeSlot（跨平面 dst 偏移会被 noop，只能 dst(0,0)）。
       若先拷条带再拷 UV，dst Y 部分会把三条带覆盖 → 亮度采样恒为源 Y 顶 4 行
       →「下半屏黑」检测失效。先 UV 后条带：条带随后重写 dst Y 0..11，UV 只读
       dst y=12..13 不受影响。 */
    D3D11_BOX boxUV = { 0, 0, 0, 64, (UINT)(frameH + 2), 1 };
    r->ctx->CopySubresourceRegion(r->lumaProbeTex, 0, 0, 0, 0,
                                  (ID3D11Resource*)r->copyTex[slot], 0, &boxUV);
    /* Y 上/中/下 3 条带各 4 行：条带起始行按帧高比例定位（1/3、2/3 处），
       单条带采不到的「半屏黑」形态由其他条带补覆盖。 */
    {
        const int bandRows = 4;  /* 每条带采样的 Y 行数 */
        int bandY[LUMA_PROBE_BANDS];
        for (int b = 0; b < LUMA_PROBE_BANDS; b++) {
            int y = (int)((double)frameH * b / LUMA_PROBE_BANDS + 0.5);
            if (y > frameH - bandRows - 1) y = frameH - bandRows - 1;
            if (y < 0) y = 0;
            bandY[b] = y;
        }
        for (int b = 0; b < LUMA_PROBE_BANDS; b++) {
            D3D11_BOX boxY = { 0, (UINT)bandY[b], 0, 64, (UINT)(bandY[b] + bandRows), 1 };
            r->ctx->CopySubresourceRegion(r->lumaProbeTex, 0, 0, (UINT)(b * bandRows), 0,
                                          (ID3D11Resource*)r->copyTex[slot], 0, &boxY);
        }
    }
    D3D11_MAPPED_SUBRESOURCE mapped;
    ZeroMemory(&mapped, sizeof(mapped));
    HRESULT hr = r->ctx->Map((ID3D11Resource*)r->lumaProbeTex, 0, D3D11_MAP_READ, 0, &mapped);
    if (FAILED(hr) || !mapped.pData) {
        LeaveCriticalSection(&r->cs);
        logMsg("[lumaProbe] Map failed: hr=0x%08X\n", (unsigned)hr);
        return -1;
    }
    int result = 0;
    const uint8_t* row = (const uint8_t*)mapped.pData;
    /* Y 3 条带 x4 行（探针纹理 y=0..11）：逐条带判定，任一条带暗点（<16）
       占比 ≥95% → bit0 置位。条带按帧高 0 / 1/3 / 2/3 定位，覆盖上中下，
       「下半屏黑/上半屏黑/中间黑带」等局部黑屏形态必然命中某条带。
       不能三带合并算总占比（下半屏黑只占 1/3 暗点，永达不到阈值 → 漏检）。
       全量判定对「90% 黑带一点内容」的脏帧会漏检（一点亮点就放行），
       占比判定容忍少量反常点，同时保留真正有内容的画面不会误判。 */
    {
        const int bandSamples = 64 * 4;  /* 每条带 256 点 */
        int dark[LUMA_PROBE_BANDS] = { 0, 0, 0 };
        for (int b = 0; b < LUMA_PROBE_BANDS; b++) {
            for (int y = 0; y < 4; y++) {
                const uint8_t* src = row + (SIZE_T)(b * 4 + y) * mapped.RowPitch;
                for (int x = 0; x < 64; x++) {
                    if (src[x] < 16) dark[b]++;
                }
            }
            if (dark[b] * 100 >= bandSamples * 95) { result |= 1; break; }
        }
    }
    /* UV 顶 2 行（探针纹理 y=12..13，NV12 chroma 首两行）：全 0 → 未初始化帧；
       全 128 → 标准黑色度 */
    {
        int allZero = 1, allFlat = 1;
        for (int y = 12; y < 14 && (allZero || allFlat); y++) {
            for (int x = 0; x < 64; x++) {
                uint8_t v = row[(SIZE_T)y * mapped.RowPitch + x];
                if (v != 0) allZero = 0;
                if (v != 128) allFlat = 0;
                if (!allZero && !allFlat) break;
            }
        }
        if (allZero) result |= 2;
        else if (allFlat) result |= 4;
    }
    r->ctx->Unmap((ID3D11Resource*)r->lumaProbeTex, 0);
    LeaveCriticalSection(&r->cs);
    return result;
}

/* D3D11R_RenderSlot：UI 线程调用，绘制指定槽位（不接触解码器纹理）。 */
R_API int D3D11R_RenderSlot(void* handle, int slot, int frameW, int frameH) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->device || !r->ctx) return -1;

    EnterCriticalSection(&r->cs);
    int ret = drawSlotInternal(r, slot, frameW, frameH);
    LeaveCriticalSection(&r->cs);
    return ret;
}


/* D3D11R_Represent：兜底重绘最近一帧并 Present（DWM surface 失效/遮挡恢复）。
   最近 300ms 内刚绘制过则跳过（正常帧流时每帧都在绘制，无需兜底）。 */
R_API int D3D11R_Represent(void* handle) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->swapChain) return -1;

    if (GetTickCount64() - r->lastDrawTick < 300) return 0;

    EnterCriticalSection(&r->cs);
    int ret = 0;
    if (r->rtv && r->ctx) {
        if (r->drawnSlot >= 0 && r->srvCopyY[r->drawnSlot] && r->srvCopyUV[r->drawnSlot]) {
            ret = drawSlotInternal(r, r->drawnSlot, r->srcW, r->srcH);
        } else if (r->bgraTex && r->srvBgra && r->srcW > 0 && r->srcH > 0) {
            /* BGRA 路径：重绘缓存的上一帧 */
            updateVertices(r);
            float black[4] = {0, 0, 0, 1};
            r->ctx->ClearRenderTargetView(r->rtv, black);
            r->ctx->OMSetRenderTargets(1, &r->rtv, NULL);
            D3D11_VIEWPORT vp = {0, 0, (FLOAT)r->width, (FLOAT)r->height, 0, 1};
            r->ctx->RSSetViewports(1, &vp);
            r->ctx->IASetInputLayout(r->inputLayout);
            r->ctx->VSSetShader(r->vs, NULL, 0);
            r->ctx->PSSetShader(r->ps, NULL, 0);
            r->ctx->PSSetShaderResources(0, 1, &r->srvBgra);
            r->ctx->PSSetSamplers(0, 1, &r->sampler);
            UINT vstride = sizeof(Vertex), offset = 0;
            r->ctx->IASetVertexBuffers(0, 1, &r->vertexBuf, &vstride, &offset);
            r->ctx->IASetPrimitiveTopology(D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
            r->ctx->Draw(6, 0);
            r->lastDrawTick = GetTickCount64();
            ret = 0;
        } else {
            ret = -1;
        }
        if (ret == 0) {
            InterlockedExchange(&r->pendingFrame, 0);
            HRESULT pr = r->swapChain->Present(1, 0);
            if (pr == DXGI_ERROR_DEVICE_REMOVED || pr == DXGI_ERROR_DEVICE_RESET) {
                ret = 1;
            } else if (FAILED(pr)) {
                logMsg("[Represent] Present failed: hr=0x%08X\n", (unsigned)pr);
                ret = -1;
            }
        }
    } else {
        ret = -1;
    }
    LeaveCriticalSection(&r->cs);
    return ret;
}

R_API int D3D11R_RenderBGRA(void* handle, const void* bgraData, int frameW, int frameH, int stride) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->device || !r->ctx || !bgraData) return -1;

    EnterCriticalSection(&r->cs);
    int ret = renderBGRAInternal(r, bgraData, frameW, frameH, stride);
    LeaveCriticalSection(&r->cs);
    return ret;
}

/* ─── Present (call from UI thread only) ────────────────────────────────── */

/* 返回值：0=成功；1=device removed/reset（调用方应重建管线）；-1=无待呈现帧（跳过）；
   其他负数=Present 失败 HRESULT。 */
R_API int D3D11R_Present(void* handle) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r || !r->swapChain) return -1;

    /* If no frame has been rendered yet, actively clear+present black.
       Skipping Present entirely lets DWM show undefined back buffer content
       (green) on window redraw/move/resize. Force black until first frame. */
    if (!InterlockedCompareExchange(&r->hasEverRendered, 0, 0)) {
        if (!TryEnterCriticalSection(&r->cs)) return -1;
        HRESULT pr = S_OK;
        if (r->rtv && r->ctx) {
            float black[4] = {0, 0, 0, 1};
            r->ctx->ClearRenderTargetView(r->rtv, black);
            pr = r->swapChain->Present(1, 0);
        }
        LeaveCriticalSection(&r->cs);
        if (pr == DXGI_ERROR_DEVICE_REMOVED || pr == DXGI_ERROR_DEVICE_RESET) return 1;
        if (FAILED(pr)) return (int)pr;
        return 0;
    }

    if (InterlockedCompareExchange(&r->pendingFrame, 0, 1) != 1) {
        return -1;
    }

    /* UI thread: use TryEnterCriticalSection to avoid blocking.
       If RenderBGRA/CopyTexture holds the lock, restore pendingFrame so the next
       timer tick retries — otherwise the frame would be silently dropped. */
    if (!TryEnterCriticalSection(&r->cs)) {
        InterlockedExchange(&r->pendingFrame, 1);
        return -1;
    }

    HRESULT pr = r->swapChain->Present(1, 0);

    /* FLIP_DISCARD + BufferCount=2: double-buffered. After Present, back buffer
       content is undefined (discarded). The next RenderBGRA/RenderSlot will
       ClearRenderTargetView before drawing, so no green flash between frames. */

    LeaveCriticalSection(&r->cs);

    if (pr == DXGI_ERROR_DEVICE_REMOVED || pr == DXGI_ERROR_DEVICE_RESET) {
        logMsg("[Present] device removed/reset: hr=0x%08X\n", (unsigned)pr);
        return 1;
    }
    if (FAILED(pr)) {
        static int failCount = 0;
        failCount++;
        if (failCount <= 3 || failCount % 30 == 0) {
            logMsg("[Present] Present failed #%d: hr=0x%08X\n", failCount, (unsigned)pr);
        }
        return (int)pr;
    }
    return 0;
}

/* ─── Set rotation ─────────────────────────────────────────────────────────── */

R_API void D3D11R_SetRotation(void* handle, int rotated) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r) return;
    EnterCriticalSection(&r->cs);
    if (r->rotated != rotated) {
        r->rotated = rotated;
        r->lbStale = 1;
    }
    LeaveCriticalSection(&r->cs);
}

/* Set fill mode: stretch video to fill window (skip letterbox). */
R_API void D3D11R_SetFill(void* handle, int fill) {
    D3D11Renderer* r = (D3D11Renderer*)handle;
    if (!r) return;
    EnterCriticalSection(&r->cs);
    if (r->fill != fill) {
        r->fill = fill;
        r->lbStale = 1;
    }
    LeaveCriticalSection(&r->cs);
}

