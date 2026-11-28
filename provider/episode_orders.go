package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Silo-Server/silo-plugin-tvdb/metadata"
)

// ErrUnknownEpisodeOrder reports an ordering the series has no seasons for.
var ErrUnknownEpisodeOrder = errors.New("tvdb: unknown episode order")

// ListEpisodeOrders returns one order per TVDB season type the series has
// seasons in. The order ID is the season-type slug TVDB uses in
// /series/{id}/episodes/{season-type}. A season type listed in seasonTypes
// without any season is left out: it would serve no episodes.
func (p *Provider) ListEpisodeOrders(ctx context.Context, req metadata.EpisodeOrdersRequest) ([]metadata.EpisodeOrder, error) {
	tvdbID := req.ProviderIDs["tvdb"]
	if tvdbID == "" {
		return nil, nil
	}
	id, err := strconv.Atoi(tvdbID)
	if err != nil {
		return nil, fmt.Errorf("tvdb: invalid TVDB ID: %w", err)
	}
	series, err := p.client.GetSeriesExtended(ctx, id)
	if err != nil {
		return nil, err
	}

	type found struct {
		typeID int
		order  metadata.EpisodeOrder
	}
	bySlug := make(map[string]*found)
	for _, season := range series.Seasons {
		slug := seasonTypeSlug(season.Type, series.SeasonTypes)
		if slug == "" {
			continue
		}
		entry, ok := bySlug[slug]
		if !ok {
			entry = &found{typeID: season.Type.ID, order: metadata.EpisodeOrder{
				ID:        slug,
				Name:      seasonTypeName(season.Type, series.SeasonTypes, slug),
				Kind:      episodeOrderKind(slug),
				IsDefault: slug == officialSeasonType,
			}}
			bySlug[slug] = entry
		}
		if season.Number > 0 {
			entry.order.SeasonCount++
		}
	}

	entries := make([]*found, 0, len(bySlug))
	for _, entry := range bySlug {
		entries = append(entries, entry)
	}
	// The default order first, then TVDB's season-type order.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].order.IsDefault != entries[j].order.IsDefault {
			return entries[i].order.IsDefault
		}
		if entries[i].typeID != entries[j].typeID {
			return entries[i].typeID < entries[j].typeID
		}
		return entries[i].order.ID < entries[j].order.ID
	})
	orders := make([]metadata.EpisodeOrder, len(entries))
	for i, entry := range entries {
		orders[i] = entry.order
	}
	return orders, nil
}

// episodeOrderSeasonType returns the TVDB season type that serves ordering.
// The empty ordering and "official" are the aired order and need no lookup;
// any other ordering must be a season type the series has seasons in.
func (p *Provider) episodeOrderSeasonType(ctx context.Context, seriesID int, ordering string) (string, error) {
	if ordering == "" || ordering == officialSeasonType {
		return officialSeasonType, nil
	}
	series, err := p.client.GetSeriesExtended(ctx, seriesID)
	if err != nil {
		return "", err
	}
	seasonType, _, err := orderingSeasons(series, seriesID, ordering)
	return seasonType, err
}

// orderingSeasons returns the season type that serves ordering and the
// series' seasons in it. The empty ordering is the aired order; any other
// ordering than the aired order must have seasons.
func orderingSeasons(series *SeriesExtendedRecord, seriesID int, ordering string) (string, []SeasonBaseRecord, error) {
	seasonType := officialSeasonType
	if ordering != "" {
		seasonType = ordering
	}
	seasons := seasonsOfType(series, seasonType)
	if len(seasons) == 0 && seasonType != officialSeasonType {
		return "", nil, fmt.Errorf("%w %q for series %d", ErrUnknownEpisodeOrder, ordering, seriesID)
	}
	return seasonType, seasons, nil
}

// seasonsOfType returns the series' seasons in the given season type.
func seasonsOfType(series *SeriesExtendedRecord, slug string) []SeasonBaseRecord {
	var seasons []SeasonBaseRecord
	for _, season := range series.Seasons {
		if seasonTypeSlug(season.Type, series.SeasonTypes) == slug {
			seasons = append(seasons, season)
		}
	}
	return seasons
}

// seasonTypeSlug returns the season type's TVDB slug ("official", "dvd", ...).
// A season record without the slug takes it from the series' seasonTypes by
// ID. Type 1 is always the official (aired) order, as isOfficialSeason has it.
func seasonTypeSlug(seasonType SeasonType, known []SeasonType) string {
	if seasonType.ID == officialSeasonTypeID {
		return officialSeasonType
	}
	if seasonType.Type != "" {
		return seasonType.Type
	}
	for _, candidate := range known {
		if candidate.ID == seasonType.ID && candidate.Type != "" {
			return candidate.Type
		}
	}
	return ""
}

// seasonTypeName names a season type: the series' own name for it
// (alternateName, such as "Netflix Order") when it has one, else TVDB's
// generic name, else the slug. Either can be on the season record or only in
// the series' seasonTypes.
func seasonTypeName(seasonType SeasonType, known []SeasonType, slug string) string {
	candidates := []SeasonType{seasonType}
	for _, candidate := range known {
		if candidate.ID == seasonType.ID {
			candidates = append(candidates, candidate)
		}
	}
	for _, candidate := range candidates {
		if candidate.AlternateName != nil && strings.TrimSpace(*candidate.AlternateName) != "" {
			return strings.TrimSpace(*candidate.AlternateName)
		}
	}
	for _, candidate := range candidates {
		if candidate.Name != "" {
			return candidate.Name
		}
	}
	return slug
}

// episodeOrderKind maps a TVDB season-type slug to the SDK's order kind.
func episodeOrderKind(slug string) string {
	switch slug {
	case officialSeasonType:
		return "aired"
	case "dvd", "altdvd":
		return "dvd"
	case "absolute":
		return "absolute"
	default:
		return "alternate"
	}
}
