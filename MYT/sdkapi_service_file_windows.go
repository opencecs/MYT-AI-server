//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// ---- Windows COM IFileOpenDialog 目录选择器 (Vista+) ----

var (
	ole32 = windows.NewLazySystemDLL("ole32.dll")
	shell = windows.NewLazySystemDLL("shell32.dll")

	procCoInitializeEx          = ole32.NewProc("CoInitializeEx")
	procCoUninitialize          = ole32.NewProc("CoUninitialize")
	procCoCreateInstance        = ole32.NewProc("CoCreateInstance")
	procSHCreateItemFromParsing = shell.NewProc("SHCreateItemFromParsingName")
	procCoTaskMemFree           = ole32.NewProc("CoTaskMemFree")
)

// COM IIDs
var (
	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4dde, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIShellItem       = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	fosPickFolders     = 0x00000020
	fosForceFilesystem = 0x00000040
	sigdnFilesysPath   = 0x80058000
	coinitApartment    = 0x2
	errorCancelled     = 0x800704C7
)

// IFileOpenDialog vtable offsets
// IUnknown: 0=QueryInterface, 1=AddRef, 2=Release
// IModalWindow: 3=Show
// IFileDialog: 4=SetFileTypes, 5=SetFileTypeIndex, 6=GetFileTypeIndex,
//
//	7=Advise, 8=Unadvise, 9=SetOptions, 10=GetOptions,
//	11=SetDefaultFolder, 12=SetFolder, 13=GetFolder, 14=GetCurrentSelection,
//	15=SetFileName, 16=GetFileName, 17=SetTitle, 18=SetOkButtonLabel,
//	19=SetFileNameLabel, 20=GetResult
//
// IFileOpenDialog: 21=GetResults, ...
const (
	vtblShow             = 3  // IModalWindow::Show
	vtblSetFileTypes     = 4  // IFileDialog::SetFileTypes
	vtblSetFileTypeIndex = 5  // IFileDialog::SetFileTypeIndex
	vtblSetOptions       = 9  // IFileDialog::SetOptions
	vtblSetDefaultFolder = 11 // IFileDialog::SetDefaultFolder
	vtblSetTitle         = 17 // IFileDialog::SetTitle
	vtblGetResult        = 20 // IFileDialog::GetResult
)

// IShellItem vtable offsets
const siVtblGetDisplayName = 5

const vtblRelease = 2

// comRelease 调用 IUnknown::Release 释放 COM 对象
func comRelease(obj uintptr) {
	if obj == 0 {
		return
	}
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	release := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblRelease)*unsafe.Sizeof(uintptr(0))))
	syscall.Syscall(release, 1, obj, 0, 0)
}

// pickFolderWindows 弹出 Windows 原生目录选择对话框
func pickFolderWindows(startPath string) (string, error) {
	// 初始化 COM
	procCoInitializeEx.Call(0, coinitApartment)
	defer procCoUninitialize.Call()

	// 创建 IFileOpenDialog 实例
	var dialog uintptr
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		1|4, // CLSCTX_INPROC_SERVER | CLSCTX_LOCAL_SERVER
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)),
		uintptr(unsafe.Pointer(&dialog)),
	)
	if hr != 0 || dialog == 0 {
		return "", fmt.Errorf("CoCreateInstance 失败: 0x%08X", hr)
	}
	defer comRelease(dialog)

	vtbl := *(*uintptr)(unsafe.Pointer(dialog))

	// SetOptions: FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM
	setOptions := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblSetOptions)*unsafe.Sizeof(uintptr(0))))
	syscall.Syscall(setOptions, 2, dialog, fosPickFolders|fosForceFilesystem, 0)

	// SetTitle
	setTitle := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblSetTitle)*unsafe.Sizeof(uintptr(0))))
	titlePtr, _ := windows.UTF16PtrFromString("请选择文件夹")
	syscall.Syscall(setTitle, 2, dialog, uintptr(unsafe.Pointer(titlePtr)), 0)

	// SetDefaultFolder (if startPath provided)
	if startPath != "" {
		var shellItem uintptr
		pathPtr, _ := windows.UTF16PtrFromString(startPath)
		hr2, _, _ := procSHCreateItemFromParsing.Call(
			uintptr(unsafe.Pointer(pathPtr)),
			0,
			uintptr(unsafe.Pointer(&iidIShellItem)),
			uintptr(unsafe.Pointer(&shellItem)),
		)
		if hr2 == 0 && shellItem != 0 {
			setDefaultFolder := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblSetDefaultFolder)*unsafe.Sizeof(uintptr(0))))
			syscall.Syscall(setDefaultFolder, 2, dialog, shellItem, 0)
			comRelease(shellItem)
		}
	}

	// Show
	show := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblShow)*unsafe.Sizeof(uintptr(0))))
	hr3, _, _ := syscall.Syscall(show, 2, dialog, 0, 0)
	if hr3 != 0 {
		if hr3 == errorCancelled {
			return "", fmt.Errorf("用户取消选择")
		}
		return "", fmt.Errorf("对话框显示失败: 0x%08X", hr3)
	}

	// GetResult
	var resultItem uintptr
	getResult := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblGetResult)*unsafe.Sizeof(uintptr(0))))
	hr4, _, _ := syscall.Syscall(getResult, 2, dialog, uintptr(unsafe.Pointer(&resultItem)), 0)
	if hr4 != 0 || resultItem == 0 {
		return "", fmt.Errorf("获取选择结果失败")
	}
	defer comRelease(resultItem)

	// IShellItem::GetDisplayName
	siVtbl := *(*uintptr)(unsafe.Pointer(resultItem))
	getDisplayName := *(*uintptr)(unsafe.Pointer(siVtbl + uintptr(siVtblGetDisplayName)*unsafe.Sizeof(uintptr(0))))
	var namePtr uintptr
	syscall.Syscall(getDisplayName, 3, resultItem, sigdnFilesysPath, uintptr(unsafe.Pointer(&namePtr)))
	if namePtr == 0 {
		return "", fmt.Errorf("获取路径名失败")
	}
	defer procCoTaskMemFree.Call(namePtr)

	selectedPath := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(namePtr)))
	return selectedPath, nil
}

// SelectDirectory 弹出 Windows 原生目录选择对话框
func (f *FileService) SelectDirectory(startPath string) (string, error) {
	path, err := pickFolderWindows(startPath)
	if err != nil {
		return "", err
	}
	return path, nil
}

// comdlgFilterEntry 对应 COMDLG_FILTERSTRUCT（IFileDialog::SetFileTypes 参数）
type comdlgFilterEntry struct {
	namePtr uintptr
	specPtr uintptr
}

// pickFileWindows 弹出 Windows 原生文件选择对话框
// title: 对话框标题，filters: 文件类型过滤器（如 "图片文件\0*.jpg;*.png;*.bmp;*.webp\0所有文件\0*.*\0"）
func pickFileWindows(startPath, title, filters string) (string, error) {
	procCoInitializeEx.Call(0, coinitApartment)
	defer procCoUninitialize.Call()

	var dialog uintptr
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		1|4,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)),
		uintptr(unsafe.Pointer(&dialog)),
	)
	if hr != 0 || dialog == 0 {
		return "", fmt.Errorf("CoCreateInstance 失败: 0x%08X", hr)
	}
	defer comRelease(dialog)

	vtbl := *(*uintptr)(unsafe.Pointer(dialog))

	// SetOptions: FOS_FORCEFILESYSTEM（不选文件夹）
	setOptions := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblSetOptions)*unsafe.Sizeof(uintptr(0))))
	syscall.Syscall(setOptions, 2, dialog, fosForceFilesystem, 0)

	// SetTitle
	setTitle := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblSetTitle)*unsafe.Sizeof(uintptr(0))))
	titlePtr, _ := windows.UTF16PtrFromString(title)
	syscall.Syscall(setTitle, 2, dialog, uintptr(unsafe.Pointer(titlePtr)), 0)

	// SetFileTypes（解析 \0 分隔的双空终止过滤字符串）
	if filters != "" {
		// 将 "\0" 分隔的过滤字符串拆分成 name/spec 对
		parts := splitDoubleNull(filters)
		numPairs := len(parts) / 2
		if numPairs > 0 {
			entries := make([]comdlgFilterEntry, numPairs)
			namePtrs := make([]uintptr, numPairs)
			specPtrs := make([]uintptr, numPairs)
			for i := 0; i < numPairs; i++ {
				n, _ := windows.UTF16PtrFromString(parts[i*2])
				s, _ := windows.UTF16PtrFromString(parts[i*2+1])
				namePtrs[i] = uintptr(unsafe.Pointer(n))
				specPtrs[i] = uintptr(unsafe.Pointer(s))
				entries[i] = comdlgFilterEntry{namePtr: namePtrs[i], specPtr: specPtrs[i]}
			}
			setFileTypes := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblSetFileTypes)*unsafe.Sizeof(uintptr(0))))
			syscall.Syscall(setFileTypes, 3, dialog, uintptr(numPairs), uintptr(unsafe.Pointer(&entries[0])))
			// 默认选第一个过滤器
			setFileTypeIndex := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblSetFileTypeIndex)*unsafe.Sizeof(uintptr(0))))
			syscall.Syscall(setFileTypeIndex, 2, dialog, 1, 0)
		}
	}

	// SetDefaultFolder
	if startPath != "" {
		var shellItem uintptr
		pathPtr, _ := windows.UTF16PtrFromString(startPath)
		hr2, _, _ := procSHCreateItemFromParsing.Call(
			uintptr(unsafe.Pointer(pathPtr)),
			0,
			uintptr(unsafe.Pointer(&iidIShellItem)),
			uintptr(unsafe.Pointer(&shellItem)),
		)
		if hr2 == 0 && shellItem != 0 {
			setDefaultFolder := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblSetDefaultFolder)*unsafe.Sizeof(uintptr(0))))
			syscall.Syscall(setDefaultFolder, 2, dialog, shellItem, 0)
			comRelease(shellItem)
		}
	}

	// Show
	show := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblShow)*unsafe.Sizeof(uintptr(0))))
	hr3, _, _ := syscall.Syscall(show, 2, dialog, 0, 0)
	if hr3 != 0 {
		if hr3 == errorCancelled {
			return "", fmt.Errorf("用户取消选择")
		}
		return "", fmt.Errorf("对话框显示失败: 0x%08X", hr3)
	}

	// GetResult
	var resultItem uintptr
	getResult := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(vtblGetResult)*unsafe.Sizeof(uintptr(0))))
	hr4, _, _ := syscall.Syscall(getResult, 2, dialog, uintptr(unsafe.Pointer(&resultItem)), 0)
	if hr4 != 0 || resultItem == 0 {
		return "", fmt.Errorf("获取选择结果失败")
	}
	defer comRelease(resultItem)

	// IShellItem::GetDisplayName
	siVtbl := *(*uintptr)(unsafe.Pointer(resultItem))
	getDisplayName := *(*uintptr)(unsafe.Pointer(siVtbl + uintptr(siVtblGetDisplayName)*unsafe.Sizeof(uintptr(0))))
	var namePtr uintptr
	syscall.Syscall(getDisplayName, 3, resultItem, sigdnFilesysPath, uintptr(unsafe.Pointer(&namePtr)))
	if namePtr == 0 {
		return "", fmt.Errorf("获取路径名失败")
	}
	defer procCoTaskMemFree.Call(namePtr)

	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(namePtr))), nil
}

// splitDoubleNull 将 "\0" 分隔的字符串拆分为字符串切片（去掉空元素）
func splitDoubleNull(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			part := s[start:i]
			if part != "" {
				result = append(result, part)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

// SelectImageFile 弹出 Windows 原生图片文件选择对话框
func (f *FileService) SelectImageFile(startPath string) (string, error) {
	const imageFilter = "图片文件\x00*.jpg;*.jpeg;*.png;*.bmp;*.webp;*.gif\x00所有文件\x00*.*\x00"
	return pickFileWindows(startPath, "请选择图片文件", imageFilter)
}

// SelectVideoFile 弹出 Windows 原生视频文件选择对话框
func (f *FileService) SelectVideoFile(startPath string) (string, error) {
	const videoFilter = "视频文件\x00*.mp4;*.avi;*.mkv;*.mov;*.wmv;*.flv;*.webm;*.3gp\x00所有文件\x00*.*\x00"
	return pickFileWindows(startPath, "请选择视频文件", videoFilter)
}

// SelectUploadFiles 弹出原生多选文件对话框，返回选中文件的 UploadFileEntry 列表
// 使用 Wails 内置文件对话框（基于 cfd 库），稳定可靠
func (f *FileService) SelectUploadFiles(startPath string) ([]UploadFileEntry, error) {
	if f.app == nil || f.app.Dialog == nil {
		return nil, fmt.Errorf("应用未初始化")
	}
	dialog := f.app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                   "请选择要上传的文件",
		CanChooseFiles:          true,
		CanChooseDirectories:    false,
		AllowsMultipleSelection: true,
		AllowsOtherFileTypes:    true,
		Directory:               startPath,
		Filters: []application.FileFilter{
			{DisplayName: "所有文件", Pattern: "*.*"},
			{DisplayName: "安装包", Pattern: "*.apk;*.apks;*.xapk"},
		},
	})
	paths, err := dialog.PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}
	return f.BuildUploadEntries(paths), nil
}
