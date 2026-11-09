//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// SelectDirectory 使用 osascript 调用 macOS "choose folder" 对话框
func (f *FileService) SelectDirectory(startPath string) (string, error) {
	script := `try
	set chosenFolder to choose folder with prompt "请选择文件夹"
	return POSIX path of chosenFolder
on error number -128
	return "CANCELLED"
end try`

	if startPath != "" {
		script = fmt.Sprintf(`try
	set chosenFolder to choose folder with prompt "请选择文件夹" default location POSIX file "%s"
	return POSIX path of chosenFolder
on error number -128
	return "CANCELLED"
end try`, startPath)
	}

	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", fmt.Errorf("目录选择对话框失败: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "CANCELLED" {
		return "", fmt.Errorf("用户取消选择")
	}

	return result, nil
}

// SelectUploadFiles 使用 osascript 调用 macOS 多选文件对话框
func (f *FileService) SelectUploadFiles(startPath string) ([]UploadFileEntry, error) {
	script := `try
	set chosenFiles to choose file with prompt "请选择要上传的文件" with multiple selections allowed
	set output to ""
	repeat with f in chosenFiles
		set output to output & (POSIX path of f) & linefeed
	end repeat
	return output
on error number -128
	return "CANCELLED"
end try`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return nil, fmt.Errorf("文件选择对话框失败: %w", err)
	}
	result := strings.TrimSpace(string(out))
	if result == "CANCELLED" || result == "" {
		return nil, fmt.Errorf("用户取消选择")
	}
	paths := make([]string, 0)
	for _, p := range strings.Split(result, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	return f.BuildUploadEntries(paths), nil
}
