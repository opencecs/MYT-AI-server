@echo off
REM Build ffdecode.dll with MinGW GCC — FFmpeg 8.1 statically linked
REM Prerequisites: MSYS2/MinGW64 in PATH (e.g. C:\msys64\mingw64\bin)
REM Run from: myt-client-player/ffdecode  OR  any directory (uses absolute paths)

set "SCRIPT_DIR=%~dp0"
set FF_INC=%SCRIPT_DIR%third_party\ffmpeg-8.1\include
set FF_LIB=%SCRIPT_DIR%third_party\ffmpeg-8.1\lib
set SRC=%SCRIPT_DIR%ffdecode.c
set OUT=%SCRIPT_DIR%..\ffdecode.dll

echo === Building ffdecode.dll (FFmpeg 8.1 H264 decoder wrapper) ===

g++ -shared -O2 -static -o "%OUT%" "%SRC%" ^
  -I"%FF_INC%" ^
  "%FF_LIB%\libavcodec.a" ^
  "%FF_LIB%\libswscale.a" ^
  "%FF_LIB%\libswresample.a" ^
  "%FF_LIB%\libavutil.a" ^
  -ld3d11 -ldxgi -ldxva2 -lws2_32 -lbcrypt -lSecur32 -lOle32 -lUser32 -lShlwapi -lAdvapi32 -lShell32 -lm ^
  -Wl,--whole-archive -lwinpthread -Wl,--no-whole-archive

if exist "%OUT%" (
    echo.
    echo SUCCESS: %OUT%
) else (
    echo.
    echo FAILED
)
