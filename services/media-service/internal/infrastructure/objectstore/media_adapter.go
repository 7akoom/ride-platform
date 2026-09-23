package objectstore

import (
	"context"
	"errors"

	"github.com/7akoom/ride-platform/services/media-service/internal/application/media"
)

// MediaStore adapts a Store to the media service's port.
type MediaStore struct {
	*Store
}

var _ media.ObjectStore = MediaStore{}

func (m MediaStore) Stat(ctx context.Context, key string) (media.ObjectInfo, error) {
	info, err := m.Store.Stat(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return media.ObjectInfo{}, media.ErrNotUploaded
	}

	if err != nil {
		return media.ObjectInfo{}, err
	}

	return media.ObjectInfo{Size: info.Size}, nil
}

func (m MediaStore) Read(ctx context.Context, key string, limit int64) ([]byte, error) {
	data, err := m.Store.Read(ctx, key, limit)

	switch {
	case errors.Is(err, ErrNotFound):
		return nil, media.ErrNotUploaded
	case errors.Is(err, ErrTooLarge):
		return nil, media.ErrObjectTooLarge
	}

	return data, err
}
