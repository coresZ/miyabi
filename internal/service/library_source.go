package service

import (
	"context"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/pan"
)

func fileInfoPath(info pan.FileInfo) string {
	return drive.FilePath(info.Path, info.Name)
}

func (service *LibraryService) sourceInfo(ctx context.Context, sess drive.Session, id string) (pan.FileInfo, error) {
	info, err := sess.Info(ctx, id)
	if err != nil {
		return pan.FileInfo{}, err
	}
	if !drive.WithinSource(info, sess.Source()) {
		return pan.FileInfo{}, domain.E(domain.KindNotFound, "下载资源已移出媒体目录", nil)
	}
	return info, nil
}

func (service *LibraryService) directoryEntries(ctx context.Context, sess drive.Session, id string) ([]pan.File, error) {
	var files []pan.File
	err := drive.WalkFilePages(ctx, func(offset int) (pan.FilePage, error) {
		page, err := sess.List(ctx, id, offset)
		if err != nil {
			return pan.FilePage{}, err
		}
		if !slices.ContainsFunc(page.Path, func(directory pan.Directory) bool { return directory.ID == sess.Source().Directory.ID }) {
			return pan.FilePage{}, domain.E(domain.KindConflict, "该文件夹已移出媒体目录，请重新扫描", nil)
		}
		return page, nil
	}, func(page pan.FilePage) (bool, error) {
		files = append(files, page.Files...)
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func sidecarByName(files []pan.File, name string) (pan.File, bool) {
	for _, entry := range files {
		if !entry.IsDirectory && strings.EqualFold(entry.Name, name) {
			return entry, true
		}
	}
	return pan.File{}, false
}
