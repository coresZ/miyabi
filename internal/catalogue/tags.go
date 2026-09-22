package catalogue

import (
	"context"
	"slices"

	"github.com/ppxb/miyabi/internal/domain"
)

// Catalogue tag IDs are global. Some details include unnamed tags whose
// definitions live in another section's taxonomy; complete them before caching.
func (service *Service) completeMovieTags(ctx context.Context, detail *domain.MovieDetail) error {
	if detail.Zone == domain.ZoneUnknown {
		// No supported taxonomy can be selected. Keep names supplied by the
		// detail without making classification-dependent requests.
		detail.Tags = namedMovieTags(detail.Tags)
		return nil
	}
	var missing []int
	for index, tag := range detail.Tags {
		if tag.Name == "" || tag.CategoryID == "" {
			missing = append(missing, index)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	zones := []domain.Zone{detail.Zone, domain.ZoneCensored, domain.ZoneUncensored, domain.ZoneWestern, domain.ZoneFC2, domain.ZoneAnime}
	for index, zone := range zones {
		if index > 0 && zone == detail.Zone {
			continue
		}
		categories, err := service.Tags(ctx, zone)
		if err != nil {
			return err
		}
		definitions := make(map[string]domain.Tag)
		for _, category := range categories {
			for _, tag := range category.Tags {
				definitions[tag.ID] = domain.Tag{Name: tag.Name, CategoryID: category.ID}
			}
		}
		pending := missing[:0]
		for _, index := range missing {
			tag := &detail.Tags[index]
			definition, found := definitions[tag.ID]
			if found {
				if tag.Name == "" {
					tag.Name = definition.Name
				}
				if tag.CategoryID == "" {
					tag.CategoryID = definition.CategoryID
				}
			}
			if tag.Name == "" || tag.CategoryID == "" {
				pending = append(pending, index)
			}
		}
		missing = pending
		if len(missing) == 0 {
			return nil
		}
	}
	// Retired tag IDs can remain on a movie without a name or taxonomy entry.
	// Keep available metadata; an unnamed optional tag cannot be displayed.
	detail.Tags = namedMovieTags(detail.Tags)
	return nil
}

func namedMovieTags(tags []domain.Tag) []domain.Tag {
	return slices.DeleteFunc(tags, func(tag domain.Tag) bool { return tag.Name == "" })
}
