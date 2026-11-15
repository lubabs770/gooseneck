package library

import (
	"errors"
	"fmt"

	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/config"
)

// EditableCatalog returns the personal (file-backed) catalog called name.
// The main catalog and URL-followed catalogs are read-only.
func EditableCatalog(cfg config.Config, name string) (config.CatalogSource, error) {
	for _, src := range cfg.Catalogs {
		if src.Name != name {
			continue
		}
		if src.URL != "" {
			return src, fmt.Errorf("%s follows a URL and is read-only", name)
		}
		return src, nil
	}
	if name == catalog.MainName {
		return config.CatalogSource{}, errors.New("the main catalog is read-only; make your own: goose catalog new mine")
	}
	return config.CatalogSource{}, fmt.Errorf("no catalog named %q — create it: goose catalog new %s", name, name)
}

// UpdateCatalogs downloads the main catalog and every enabled URL catalog,
// calling fn with each one's artist count or error. It returns the last error.
func UpdateCatalogs(cfg config.Config, fn func(name string, n int, err error)) error {
	dest := config.CatalogCachePath()
	if err := catalog.Download(cfg.ArtistsURL, dest); err != nil {
		return err
	}
	main, err := catalog.Read(dest)
	if err != nil {
		return err
	}
	fn(catalog.MainName, main.Len(), nil)
	var failed error
	for _, src := range cfg.Catalogs {
		if src.URL == "" || src.Disabled {
			continue
		}
		if err := catalog.Download(src.URL, src.File()); err != nil {
			failed = err
			fn(src.Name, 0, err)
			continue
		}
		c, err := catalog.Read(src.File())
		if err != nil {
			failed = err
			fn(src.Name, 0, err)
			continue
		}
		fn(src.Name, c.Len(), nil)
	}
	return failed
}
