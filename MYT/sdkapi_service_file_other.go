//go:build !windows && !darwin

package main

import "fmt"

// SelectDirectory 不支持当前平台的目录选择
func (f *FileService) SelectDirectory(startPath string) (string, error) {
	return "", fmt.Errorf("目录选择对话框不支持当前平台")
}

// SelectUploadFiles 不支持当前平台的文件多选
func (f *FileService) SelectUploadFiles(startPath string) ([]UploadFileEntry, error) {
	return nil, fmt.Errorf("文件多选对话框不支持当前平台")
}
