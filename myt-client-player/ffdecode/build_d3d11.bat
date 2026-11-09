@echo off
REM Build d3d11render.dll with MSVC
REM Prerequisites: Visual Studio Build Tools 2022
REM Run from: myt-client-player/ffdecode  OR  any directory (uses absolute paths)

set MSVC_ROOT=C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Tools\MSVC\14.44.35207
set MSVC_BIN=%MSVC_ROOT%\bin\Hostx64\x64
set SDK_ROOT=C:\Program Files (x86)\Windows Kits\10
set SDK_VER=10.0.26100.0

REM 自动检测脚本所在目录（支持中文路径）
set "SCRIPT_DIR=%~dp0"
set "SRC=%SCRIPT_DIR%d3d11render.cpp"
set "DEF=%SCRIPT_DIR%d3d11render.def"
set "OUT=%SCRIPT_DIR%..\d3d11render.dll"

echo === Building d3d11render.dll (D3D11 GPU renderer) ===

"%MSVC_BIN%\cl.exe" /LD /utf-8 /O2 /MT /EHa ^
  "%SRC%" ^
  /I"%MSVC_ROOT%\include" ^
  /I"%SDK_ROOT%\Include\%SDK_VER%\ucrt" ^
  /I"%SDK_ROOT%\Include\%SDK_VER%\um" ^
  /I"%SDK_ROOT%\Include\%SDK_VER%\shared" ^
  /link ^
  /LIBPATH:"%MSVC_ROOT%\lib\x64" ^
  /LIBPATH:"%SDK_ROOT%\Lib\%SDK_VER%\ucrt\x64" ^
  /LIBPATH:"%SDK_ROOT%\Lib\%SDK_VER%\um\x64" ^
  d3d11.lib dxgi.lib d3dcompiler.lib User32.lib ^
  /DEF:"%DEF%" ^
  /OUT:"%OUT%"

if exist "%OUT%" (
    echo.
    echo SUCCESS: %OUT%
    del /q d3d11render.obj d3d11render.exp d3d11render.lib 2>nul
) else (
    echo.
    echo FAILED
)
