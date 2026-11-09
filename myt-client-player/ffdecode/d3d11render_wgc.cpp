/*
 * d3d11render_wgc.cpp — Windows.Graphics.Capture（WGC）采集，扫码伪源
 *
 * 两个采集模式（共用同一组静态单例 session，Go 侧保证同一时刻只用一个源）：
 *   1. D3D11R_WgcInit(HWND)        CreateForWindow：抓 DWM 合成前的窗口内容
 *                                  （「被遮挡的窗口」源，被盖住也能拿到完整画面）；
 *   2. D3D11R_WgcInitMonitor(HMONITOR) CreateForMonitor：抓显示器合成桌面，
 *                                  配合 Go 侧 SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE)
 *                                  把投屏窗口从捕获里抠掉，再按窗口矩形裁剪——
 *                                  得到的就是「被窗口挡住的真实桌面内容」（camera_push_screen.go）。
 *
 * 与主渲染器（d3d11render.cpp）解耦：独立静态句柄、自建 D3D11 device
 * （BGRA_SUPPORT，capture 管线不渲染只拷贝回读），互不牵连。
 *
 * 编译选项与 d3d11render.cpp 相同（/std:c++17 兼容旧编译脚本），链接
 * WindowsApp.lib（RoGetActivationFactory 等 WinRT 基础设施）。
 *
 * 导出（见 d3d11render.def）：
 *   D3D11R_WgcInit(HWND)          初始化窗口 capture session
 *   D3D11R_WgcInitMonitor(HMONITOR) 初始化显示器 capture session
 *   D3D11R_WgcFrameSize(int*,int*)  查询当前 session 采集尺寸（item 尺寸，物理 px）
 *   D3D11R_WgcGrabFrame(...)        取最新帧 BGRA → outBuf（顶行在前，紧凑 RowPitch=W*4）
 *   D3D11R_WgcGrabFrameRect(...)    取最新帧并按像素矩形裁剪/缩放 → dstW x dstH
 *   D3D11R_WgcClose()               释放 session
 *
 * 失败路径：Win10 <1903 / API 不可用返回负错误码，Go 侧按不可用降级。
 */

#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdio.h>
#include <windows.h>
#include <d3d11.h>
#include <dxgi1_2.h>

#include <wrl.h>

/* WinRT ABI（C 接口层）：SDK winrt/ 目录下的 MIDL 生成头（/I winrt 目录）。
   全部走 ABI:: 限定名调用（RoGetActivationFactory + interop 接口），
   不用 C++/WinRT（cppwinrt 目录）投影——免 /std:c++17 与 C++/WinRT
   模板开销，纯 vtable 调用更接近现有 C 风格代码。 */
#include <inspectable.h>
#include <windows.graphics.capture.h>
#include <Windows.Graphics.Capture.Interop.h>
#include <windows.graphics.directx.direct3d11.interop.h>

namespace {
    /* 自建 D3D11 device（BGRA_SUPPORT）+ WinRT IDirect3DDevice 包装 */
    Microsoft::WRL::ComPtr<ID3D11Device>          g_device;
    Microsoft::WRL::ComPtr<ID3D11DeviceContext>   g_ctx;
    Microsoft::WRL::ComPtr<ABI::Windows::Graphics::DirectX::Direct3D11::IDirect3DDevice> g_winrtDevice;
    Microsoft::WRL::ComPtr<ABI::Windows::Graphics::Capture::IDirect3D11CaptureFramePool> g_framePool;
    Microsoft::WRL::ComPtr<ABI::Windows::Graphics::Capture::IGraphicsCaptureSession>     g_session;
    Microsoft::WRL::ComPtr<ABI::Windows::Graphics::Capture::IGraphicsCaptureItem>        g_item;
    /* 回读 staging：BGRA STAGING，尺寸变化才重建。与 frame pool GPU 帧同 device。 */
    Microsoft::WRL::ComPtr<ID3D11Texture2D>       g_staging;
    UINT                                          g_stagingW = 0, g_stagingH = 0;
    HWND                                          g_hwnd = NULL;
}

/* GetDXGIInterfaceFromObject 手工展开：IInspectable → IDirect3DDxgiInterfaceAccess
   → 拿出 D3D11 纹理指针（frame pool 产出的 GPU BGRA 帧表面）。 */
static HRESULT wgcSurfaceToTexture(Microsoft::WRL::ComPtr<::IInspectable> const& inspectable, ID3D11Texture2D** outTex) {
    *outTex = NULL;
    Microsoft::WRL::ComPtr<Windows::Graphics::DirectX::Direct3D11::IDirect3DDxgiInterfaceAccess> access;
    HRESULT hr = inspectable.As(&access);
    if (FAILED(hr)) return hr;
    return access->GetInterface(__uuidof(ID3D11Texture2D), (void**)outTex);
}

/* 初始化 D3D11 device（自建，与渲染器独立；WGC capture 管线不渲染只回读） */
static HRESULT wgcCreateDevice() {
    UINT flags = D3D11_CREATE_DEVICE_BGRA_SUPPORT;
    return D3D11CreateDevice(
        NULL, D3D_DRIVER_TYPE_HARDWARE, NULL, flags,
        NULL, 0, D3D11_SDK_VERSION,
        g_device.GetAddressOf(), NULL, g_ctx.GetAddressOf());
}

/* wgcEnsureRuntime 版本检查 + device + RoInitialize + WinRT IDirect3DDevice 包装。
   两个 Init 入口的公共前段。返回 0 成功；-1 系统/参数不支持；-2 device 失败；
   -3 activation 失败。 */
static int wgcEnsureRuntime() {
    /* Win10 1903+ 才有 CreateForWindow/CreateForMonitor / CreateFreeThreaded（contract 6.0）。
       RtlGetVersion 声明在 wdm/ntddk（SDK 不随桌面应用头分发），改为
       动态加载 ntdll——免额外 SDK 头依赖，桌面应用标准做法。 */
    typedef LONG(WINAPI* FnRtlGetVersion)(PRTL_OSVERSIONINFOW);
    RTL_OSVERSIONINFOW vi = {};
    vi.dwOSVersionInfoSize = sizeof(vi);
    HMODULE ntdll = GetModuleHandleW(L"ntdll.dll");
    FnRtlGetVersion rtlGetVersion =
        ntdll ? (FnRtlGetVersion)(void*)GetProcAddress(ntdll, "RtlGetVersion") : NULL;
    if (!rtlGetVersion || FAILED(rtlGetVersion(&vi)) ||
        vi.dwMajorVersion < 10 || vi.dwBuildNumber < 18362) return -1;

    HRESULT hr = wgcCreateDevice();
    if (FAILED(hr)) return -2;

    /* RoInitialize（多线程，进程内幂等可重复） */
    hr = RoInitialize(RO_INIT_MULTITHREADED);
    if (FAILED(hr) && hr != RPC_E_CHANGED_MODE && hr != S_FALSE) return -3;

    /* DXGI device → WinRT IDirect3DDevice 包装（interop 标准流程） */
    Microsoft::WRL::ComPtr<IDXGIDevice> dxgiDevice;
    hr = g_device.As(&dxgiDevice);
    if (FAILED(hr)) return -2;
    Microsoft::WRL::ComPtr<::IInspectable> inspectable;
    hr = CreateDirect3D11DeviceFromDXGIDevice(dxgiDevice.Get(), inspectable.GetAddressOf());
    if (FAILED(hr)) return -3;
    hr = inspectable.As(&g_winrtDevice);
    if (FAILED(hr)) return -3;
    return 0;
}

/* wgcSetupWithItem 对已创建的 capture item 建 frame pool + session 并开始采集。
   两个 Init 入口的公共后段。返回 0 成功；-4 item 转换失败；-5 frame pool/session 失败。
   Go 侧负责在重新 init 前先调 WgcClose（dll 不重复关旧 session）。 */
static int wgcSetupWithItem(Microsoft::WRL::ComPtr<::IInspectable>& itemInspectable) {
    HRESULT hr = itemInspectable.As(&g_item);
    if (FAILED(hr)) return -4;

    /* frame pool：CreateFreeThreaded（无 DispatcherQueue 依赖）+ StartCapture。
       TryGetNextFrame 轮询模式（15fps 帧泵节奏），不用 FrameArrived 回调线程。 */
    Microsoft::WRL::ComPtr<ABI::Windows::Graphics::Capture::IDirect3D11CaptureFramePoolStatics2> poolStatics;
    hr = RoGetActivationFactory(
        Microsoft::WRL::Wrappers::HStringReference(
            RuntimeClass_Windows_Graphics_Capture_Direct3D11CaptureFramePool).Get(),
        IID_PPV_ARGS(poolStatics.GetAddressOf()));
    if (FAILED(hr)) return -5;
    ABI::Windows::Graphics::SizeInt32 size = {};
    hr = g_item->get_Size(&size);
    if (FAILED(hr)) return -5;
    hr = poolStatics->CreateFreeThreaded(
        g_winrtDevice.Get(),
        ABI::Windows::Graphics::DirectX::DirectXPixelFormat_B8G8R8A8UIntNormalized,
        2, size, g_framePool.GetAddressOf());
    if (FAILED(hr)) return -5;
    hr = g_framePool->CreateCaptureSession(g_item.Get(), g_session.GetAddressOf());
    if (FAILED(hr)) return -5;
    /* 关黄色边框：Win11+（contract 12.0）支持；旧系统 QI 失败直接忽略 */
    Microsoft::WRL::ComPtr<ABI::Windows::Graphics::Capture::IGraphicsCaptureSession3> s3;
    if (SUCCEEDED(g_session.As(&s3))) s3->put_IsBorderRequired(FALSE);
    g_session->StartCapture();
    return 0;
}

extern "C" {

/* D3D11R_WgcInit 初始化对指定窗口的 WGC capture session。
   返回 0 成功；-1 参数/系统不支持；-2 device 创建失败；-3 activation 失败；
   -4 CreateForWindow 失败；-5 frame pool/session 失败。 */
__declspec(dllexport) int D3D11R_WgcInit(void* hwndVoid) {
    HWND hwnd = (HWND)hwndVoid;
    if (!hwnd || !IsWindow(hwnd)) return -1;

    int rc = wgcEnsureRuntime();
    if (rc != 0) return rc;

    /* GraphicsCaptureItem.CreateForWindow（经 interop 接口，绕过 UWP 前置） */
    Microsoft::WRL::ComPtr<IGraphicsCaptureItemInterop> interop;
    HRESULT hr = RoGetActivationFactory(
        Microsoft::WRL::Wrappers::HStringReference(
            RuntimeClass_Windows_Graphics_Capture_GraphicsCaptureItem).Get(),
        IID_PPV_ARGS(interop.GetAddressOf()));
    if (FAILED(hr)) return -4;
    Microsoft::WRL::ComPtr<::IInspectable> itemInspectable;
    hr = interop->CreateForWindow(hwnd, IID_PPV_ARGS(itemInspectable.GetAddressOf()));
    if (FAILED(hr)) return -4;

    int rc2 = wgcSetupWithItem(itemInspectable);
    if (rc2 != 0) return rc2;
    g_hwnd = hwnd;
    return 0;
}

/* D3D11R_WgcInitMonitor 初始化对指定显示器的 WGC capture session
   （CreateForMonitor，帧尺寸 = 显示器物理像素）。
   返回码同 WgcInit（-4 = CreateForMonitor 失败）。 */
__declspec(dllexport) int D3D11R_WgcInitMonitor(void* hmonitorVoid) {
    HMONITOR hmon = (HMONITOR)hmonitorVoid;
    if (!hmon) return -1;

    int rc = wgcEnsureRuntime();
    if (rc != 0) return rc;

    Microsoft::WRL::ComPtr<IGraphicsCaptureItemInterop> interop;
    HRESULT hr = RoGetActivationFactory(
        Microsoft::WRL::Wrappers::HStringReference(
            RuntimeClass_Windows_Graphics_Capture_GraphicsCaptureItem).Get(),
        IID_PPV_ARGS(interop.GetAddressOf()));
    if (FAILED(hr)) return -4;
    Microsoft::WRL::ComPtr<::IInspectable> itemInspectable;
    hr = interop->CreateForMonitor(hmon, IID_PPV_ARGS(itemInspectable.GetAddressOf()));
    if (FAILED(hr)) return -4;

    int rc2 = wgcSetupWithItem(itemInspectable);
    if (rc2 != 0) return rc2;
    g_hwnd = NULL;
    return 0;
}

/* D3D11R_WgcFrameSize 查询当前 session 的采集尺寸（item 尺寸，物理 px；
   监视器模式即显示器物理分辨率——Go 侧用它把 GetWindowRect 逻辑矩形换算成帧内像素）。 */
__declspec(dllexport) int D3D11R_WgcFrameSize(int* outW, int* outH) {
    if (outW) *outW = 0;
    if (outH) *outH = 0;
    if (!g_item) return -2;
    if (!outW || !outH) return -3;
    ABI::Windows::Graphics::SizeInt32 size = {};
    HRESULT hr = g_item->get_Size(&size);
    if (FAILED(hr)) return -4;
    *outW = (int)size.Width;
    *outH = (int)size.Height;
    return 0;
}

/* wgcCopyLatestToStaging 取最新帧并整帧拷入 staging（BGRA）。
   返回 0 成功（*outW/*outH = 帧尺寸）；-1 暂无新帧；-2 未初始化；-4 失败。 */
static int wgcCopyLatestToStaging(UINT* outW, UINT* outH) {
    *outW = 0; *outH = 0;
    if (!g_framePool) return -2;

    Microsoft::WRL::ComPtr<ABI::Windows::Graphics::Capture::IDirect3D11CaptureFrame> frame;
    HRESULT hr = g_framePool->TryGetNextFrame(frame.GetAddressOf());
    if (FAILED(hr)) return -4;
    if (!frame) return -1;

    Microsoft::WRL::ComPtr<ABI::Windows::Graphics::DirectX::Direct3D11::IDirect3DSurface> surface;
    hr = frame->get_Surface(surface.GetAddressOf());
    if (FAILED(hr) || !surface) return -4;
    Microsoft::WRL::ComPtr<::IInspectable> surfaceInspectable;
    hr = surface.As(&surfaceInspectable);
    if (FAILED(hr)) return -4;
    Microsoft::WRL::ComPtr<ID3D11Texture2D> tex;
    hr = wgcSurfaceToTexture(surfaceInspectable, tex.GetAddressOf());
    if (FAILED(hr) || !tex) return -4;

    D3D11_TEXTURE2D_DESC desc;
    tex->GetDesc(&desc);
    UINT w = desc.Width, h = desc.Height;

    /* staging 回读（BGRA STAGING，尺寸变化才重建） */
    if (!g_staging || g_stagingW != w || g_stagingH != h) {
        D3D11_TEXTURE2D_DESC sd = desc;
        sd.Usage = D3D11_USAGE_STAGING;
        sd.BindFlags = 0;
        sd.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
        sd.MiscFlags = 0;
        sd.MipLevels = 1;
        sd.ArraySize = 1;
        sd.SampleDesc.Count = 1;
        sd.SampleDesc.Quality = 0;
        g_staging = nullptr;
        hr = g_device->CreateTexture2D(&sd, NULL, g_staging.GetAddressOf());
        if (FAILED(hr)) return -4;
        g_stagingW = w;
        g_stagingH = h;
    }
    g_ctx->CopyResource(g_staging.Get(), tex.Get());
    *outW = w;
    *outH = h;
    return 0;
}

/* D3D11R_WgcGrabFrame 取最新帧。
   outBuf 容量 outBufSize 字节；返回 0 成功，-1 暂无新帧（帧泵按节奏继续），
   -2 未初始化，-3 outBuf 太小，-4 失败（session 失效等）。
   布局：BGRA 顶行在前，紧凑无 padding（W*H*4）。 */
__declspec(dllexport) int D3D11R_WgcGrabFrame(uint8_t* outBuf, int outBufSize,
                                              int* outW, int* outH) {
    if (outW) *outW = 0;
    if (outH) *outH = 0;
    if (!g_framePool) return -2;
    if (!outBuf || !outW || !outH) return -3;

    UINT w, h;
    int rc = wgcCopyLatestToStaging(&w, &h);
    if (rc != 0) return rc;
    if (outBufSize < (int)(w * h * 4)) return -3;

    D3D11_MAPPED_SUBRESOURCE mapped;
    ZeroMemory(&mapped, sizeof(mapped));
    HRESULT hr = g_ctx->Map(g_staging.Get(), 0, D3D11_MAP_READ, 0, &mapped);
    if (FAILED(hr)) return -4;
    /* 逐行拷出（RowPitch 可能带 padding），紧凑 packing：W*H*4 */
    const uint8_t* src = (const uint8_t*)mapped.pData;
    for (UINT y = 0; y < h; y++) {
        memcpy(outBuf + (SIZE_T)w * 4 * y, src + (SIZE_T)mapped.RowPitch * y, (SIZE_T)w * 4);
    }
    g_ctx->Unmap(g_staging.Get(), 0);

    if (outW) *outW = (int)w;
    if (outH) *outH = (int)h;
    return 0;
}

/* D3D11R_WgcGrabFrameRect 取最新帧并按 src 像素矩形裁剪 → 最近邻缩放到 dstW x dstH。
   srcX/Y/W/H 为帧内像素矩形（调用方按 GetWindowRect 逻辑矩形 × 帧尺寸/显示器逻辑尺寸
   换算，见 camera_push_wgc.go）；越界部分自动 clamp、输出对应区域为黑（0）。
   返回 0 成功；-1 暂无新帧；-2 未初始化；-3 参数/缓冲无效；-4 失败；-5 src 矩形无效。
   布局：BGRA 顶行在前，紧凑无 padding（dstW*dstH*4）。 */
__declspec(dllexport) int D3D11R_WgcGrabFrameRect(uint8_t* outBuf, int outBufSize,
                                                  int dstW, int dstH,
                                                  int srcX, int srcY, int srcW, int srcH) {
    if (!g_framePool) return -2;
    if (!outBuf || dstW <= 0 || dstH <= 0 || outBufSize < dstW * dstH * 4) return -3;

    UINT fw, fh;
    int rc = wgcCopyLatestToStaging(&fw, &fh);
    if (rc != 0) return rc;

    /* clamp src 到帧内 */
    int x0 = srcX < 0 ? 0 : srcX;
    int y0 = srcY < 0 ? 0 : srcY;
    int x1 = srcX + srcW; if (x1 > (int)fw) x1 = (int)fw;
    int y1 = srcY + srcH; if (y1 > (int)fh) y1 = (int)fh;
    if (x1 - x0 <= 0 || y1 - y0 <= 0) return -5; /* 窗口完全移出捕获范围 */
    int sw = x1 - x0, sh = y1 - y0;

    D3D11_MAPPED_SUBRESOURCE mapped;
    ZeroMemory(&mapped, sizeof(mapped));
    HRESULT hr = g_ctx->Map(g_staging.Get(), 0, D3D11_MAP_READ, 0, &mapped);
    if (FAILED(hr)) return -4;
    /* 最近邻采样（与 Go 侧 normalizeBGRA 同策略，QR 码条形锐度优先）；
       越界区域保持 memset 的黑。 */
    memset(outBuf, 0, (SIZE_T)dstW * dstH * 4);
    const uint8_t* base = (const uint8_t*)mapped.pData;
    for (int dy = 0; dy < dstH; dy++) {
        int sy = y0 + (int)((int64_t)sh * dy / dstH);
        const uint8_t* srow = base + (SIZE_T)mapped.RowPitch * sy;
        uint8_t* drow = outBuf + (SIZE_T)dstW * 4 * dy;
        for (int dx = 0; dx < dstW; dx++) {
            int sx = x0 + (int)((int64_t)sw * dx / dstW);
            memcpy(drow + (SIZE_T)dx * 4, srow + (SIZE_T)sx * 4, 4);
        }
    }
    g_ctx->Unmap(g_staging.Get(), 0);
    return 0;
}

/* D3D11R_WgcClose 释放全部资源（session/pool/staging/device） */
__declspec(dllexport) void D3D11R_WgcClose(void) {
    g_session = nullptr;
    g_framePool = nullptr;
    g_item = nullptr;
    g_staging = nullptr;
    g_winrtDevice = nullptr;
    g_ctx = nullptr;
    g_device = nullptr;
    g_hwnd = NULL;
}

} /* extern "C" */
