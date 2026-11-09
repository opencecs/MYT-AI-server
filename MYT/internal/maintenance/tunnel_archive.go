package maintenance

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// 公网穿透的二进制来自同一个合并 tar.gz（内含 frpc-arm64 与 frps-amd64），
// 下面两个助手按成员名解出各自那一份并重命名（frpc / frps）。

// extractFRPCFromTarGz 从合并tar.gz包中解压frpc-arm64并重命名为frpc
func extractFRPCFromTarGz(tarPath, targetDir string) error {
	return extractMemberFromTarGz(tarPath, targetDir, "frpc-arm64", "frpc", "[公网穿透]")
}

// extractFRPSFromTarGz 从合并tar.gz包中解压frps-amd64并重命名为frps
func extractFRPSFromTarGz(tarPath, targetDir string) error {
	return extractMemberFromTarGz(tarPath, targetDir, "frps-amd64", "frps", "[公网穿透服务端]")
}

// extractMemberFromTarGz 从 tar.gz 中解出指定成员文件，重命名后落盘并置可执行权限。
func extractMemberFromTarGz(tarPath, targetDir, memberName, outName, logTag string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("打开tar.gz文件失败: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("解压gzip失败: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取tar失败: %w", err)
		}
		if filepath.Base(hdr.Name) != memberName || hdr.Typeflag != tar.TypeReg {
			continue
		}
		dstPath := filepath.Join(targetDir, outName)
		dst, err := os.Create(dstPath)
		if err != nil {
			return fmt.Errorf("创建文件 %s 失败: %w", dstPath, err)
		}
		if _, err := io.Copy(dst, tr); err != nil {
			dst.Close()
			return fmt.Errorf("写入文件 %s 失败: %w", dstPath, err)
		}
		dst.Close()
		os.Chmod(dstPath, 0755)
		log.Printf("%s 解压文件: %s -> %s", logTag, hdr.Name, outName)
		found = true
	}
	if !found {
		return fmt.Errorf("tar.gz包中未找到%s文件", memberName)
	}
	return nil
}
