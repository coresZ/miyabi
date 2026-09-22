package database

import (
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent/file"
	"github.com/ppxb/miyabi/internal/ent/predicate"
	"github.com/ppxb/miyabi/internal/ent/watchhistory"
)

// LibraryFiles scopes file rows to the account and root directory of a mounted source.
func LibraryFiles(source domain.LibrarySource) predicate.File {
	return file.And(file.AccountIDEQ(source.AccountID), file.RootIDEQ(source.Directory.ID))
}

// WatchHistory scopes watch-history rows to the account and root directory of a mounted source.
func WatchHistory(source domain.LibrarySource) predicate.WatchHistory {
	return watchhistory.And(watchhistory.AccountIDEQ(source.AccountID), watchhistory.RootIDEQ(source.Directory.ID))
}
