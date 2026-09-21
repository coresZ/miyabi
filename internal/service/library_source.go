package service

import (
	"context"

	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/pan"
)

func fileInfoPath(info pan.FileInfo) string {
	return drive.FilePath(info.Path, info.Name)
}

func (service *LibraryService) sourceInfo(ctx context.Context, sess drive.Session, id string) (pan.FileInfo, error) {
	return drive.SourceInfo(ctx, sess, id)
}

func (service *LibraryService) directoryEntries(ctx context.Context, sess drive.Session, id string) ([]pan.File, error) {
	return drive.DirectoryEntries(ctx, sess, id)
}

func sidecarByName(files []pan.File, name string) (pan.File, bool) {
	return scrape.SidecarByName(files, name)
}
