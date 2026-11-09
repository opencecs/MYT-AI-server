//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

type copyDataStruct struct {
	DwData uintptr
	CbData uint32
	LpData uintptr
}

const matrixCopyDataPrimary uintptr = 0x4d595450 // MYTP

func (ui *WindowsUI) BroadcastPrimary(addr string) {
	if addr == "" {
		return
	}
	msg := "primary:" + addr
	utf16, _ := syscall.UTF16FromString(msg)
	cds := copyDataStruct{
		DwData: matrixCopyDataPrimary,
		CbData: uint32(len(utf16) * 2),
		LpData: uintptr(unsafe.Pointer(&utf16[0])),
	}
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var cls [256]uint16
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 255)
		if syscall.UTF16ToString(cls[:]) == matrixClassName {
			procSendMessageW.Call(hwnd, WM_COPYDATA, uintptr(ui.hParent), uintptr(unsafe.Pointer(&cds)))
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
}

func (ui *WindowsUI) handleCopyData(lParam uintptr) uintptr {
	cds := (*copyDataStruct)(unsafe.Pointer(lParam))
	if cds == nil || cds.DwData != matrixCopyDataPrimary || cds.LpData == 0 || cds.CbData < 2 {
		return 0
	}
	count := int(cds.CbData / 2)
	buf := unsafe.Slice((*uint16)(unsafe.Pointer(cds.LpData)), count)
	msg := syscall.UTF16ToString(buf)
	if primary, ok := strings.CutPrefix(msg, "primary:"); ok && primary != "" {
		ui.app.SetPrimaryAddr(primary)
		ui.syncStateToPanel()
		return 1
	}
	return 0
}
