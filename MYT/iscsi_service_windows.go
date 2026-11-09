//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

// ListLocalDisks 获取本机（Windows）磁盘分区列表
func (s *IscsiServiceWrapper) ListLocalDisks() ([]LocalDiskInfo, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getLogicalDrives := kernel32.NewProc("GetLogicalDrives")
	ret, _, _ := getLogicalDrives.Call()
	bits := uint32(ret)

	var result []LocalDiskInfo
	for i := 0; i < 26; i++ {
		if bits&(1<<i) == 0 {
			continue
		}
		drive := string(rune('A'+i)) + ":\\"
		var free, total, totalFree uint64
		driveUTF16, _ := syscall.UTF16PtrFromString(drive)
		kernel32.NewProc("GetDiskFreeSpaceExW").Call(
			uintptr(unsafe.Pointer(driveUTF16)),
			uintptr(unsafe.Pointer(&free)),
			uintptr(unsafe.Pointer(&total)),
			uintptr(unsafe.Pointer(&totalFree)),
		)
		if total == 0 {
			continue
		}
		var labelBuf [256]uint16
		driveUTF16_2, _ := syscall.UTF16PtrFromString(drive)
		kernel32.NewProc("GetVolumeInformationW").Call(
			uintptr(unsafe.Pointer(driveUTF16_2)),
			uintptr(unsafe.Pointer(&labelBuf[0])),
			256,
			0, 0, 0, 0, 0,
		)
		result = append(result, LocalDiskInfo{
			Drive:      drive,
			Label:      syscall.UTF16ToString(labelBuf[:]),
			TotalBytes: total,
			FreeBytes:  free,
		})
	}
	return result, nil
}

func isVirtualNic(name, mac string) bool {
	virtualPrefixes := []string{
		"vEthernet", "vEth",
		"WSL", "vnic",
		"docker", "br-", "veth",
		"Hyper-V",
	}
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	if mac == "" || mac == "00:00:00:00:00:00" {
		return true
	}
	return false
}
