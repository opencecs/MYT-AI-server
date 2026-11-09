Unicode true

!include "wails_tools.nsh"

# LZMA 高强度压缩
SetCompressor /SOLID lzma

# 签名工具修改 EXE 后 NSIS 内嵌 CRC 会失效，改用 Authenticode 签名保证完整性
CRCCheck off

VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} 安装程序"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING

# 欢迎页标题和描述
!define MUI_WELCOMEPAGE_TITLE "欢迎使用 ${INFO_PRODUCTNAME} 安装向导"
!define MUI_WELCOMEPAGE_TEXT "此向导将引导您完成 ${INFO_PRODUCTNAME} 的安装。\r\n\r\n建议在安装前关闭所有其他应用程序。\r\n\r\n点击「下一步」继续。"

# 安装完成页
!define MUI_FINISHPAGE_TITLE "${INFO_PRODUCTNAME} 安装完成"
!define MUI_FINISHPAGE_TEXT "${INFO_PRODUCTNAME} 已成功安装到您的计算机。\r\n\r\n点击「完成」退出安装向导。"
!define MUI_FINISHPAGE_LINK "访问魔云腾官网：www.opencecs.com"
!define MUI_FINISHPAGE_LINK_LOCATION "https://www.opencecs.com"
!define MUI_FINISHPAGE_LINK_COLOR "0x0066CC"

# 目录选择页
!define MUI_DIRECTORYPAGE_TEXT_TOP "选择 ${INFO_PRODUCTNAME} 的安装目录，点击「安装」开始。"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

# 卸载页面
!define MUI_UNFINISHPAGE_TEXT "${INFO_PRODUCTNAME} 已从您的计算机卸载。"
!insertmacro MUI_UNPAGE_INSTFILES

# 使用简体中文语言包
!insertmacro MUI_LANGUAGE "SimpChinese"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show

# 检测 myt.exe 是否正在运行，如果在运行则提示并中止
!macro CheckRunning action
    nsExec::ExecToStack `tasklist /FI "IMAGENAME eq myt.exe" /NH`
    Pop $0
    Pop $1
    StrCmp $0 "0" 0 _crDone
    StrCpy $2 "myt.exe"
    StrLen $3 $2
    StrCpy $4 0
    _crLoop:
      StrCpy $5 $1 $3 $4
      StrCmp $5 $2 _crFound
      IntOp $4 $4 + 1
      StrCmp $4 200 _crDone _crLoop
    _crFound:
      MessageBox MB_OK|MB_ICONSTOP "程序正在运行，请先关闭 魔云腾-AI 再${action}。" /SD IDOK
      Abort
    _crDone:
!macroend

Function .onInit
   !insertmacro wails.checkArchitecture
   !insertmacro CheckRunning "安装"
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    # 给安装目录赋予 Users 组完全控制权限
    ExecWait 'icacls "$INSTDIR" /grant Users:(OI)(CI)F /T /Q'

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Function un.onInit
    !insertmacro CheckRunning "卸载"
FunctionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    RMDir /r "$INSTDIR\*.*"

    !insertmacro wails.deleteUninstaller

    Delete /REBOOTOK "$INSTDIR\uninstall.exe"
    RMDir /REBOOTOK "$INSTDIR"
SectionEnd
