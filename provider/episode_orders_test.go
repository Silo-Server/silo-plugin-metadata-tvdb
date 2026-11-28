package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Silo-Server/silo-plugin-tvdb/metadata"
)

// newEpisodeOrdersTestServer serves series 500 in three TVDB season types.
// Aired order has specials plus two seasons; the DVD release folds all five
// regular episodes into one season and swaps the first two; absolute order is
// one season counted 1..5. TVDB lists "alternate" in seasonTypes, but the
// series has no alternate seasons. requests counts every GET by path.
func newEpisodeOrdersTestServer(t *testing.T, requests map[string]*atomic.Int32) *httptest.Server {
	t.Helper()
	seasonType := func(id int, slug, name string) map[string]any {
		return map[string]any{"id": id, "type": slug, "name": name, "alternateName": nil}
	}
	official := seasonType(1, "official", "Aired Order")
	dvd := seasonType(2, "dvd", "DVD Order")
	absolute := seasonType(3, "absolute", "Absolute Order")
	alternate := seasonType(4, "alternate", "Alternate Order")

	episode := func(id, season, number, absoluteNumber int, name string) map[string]any {
		return map[string]any{
			"id": id, "seriesId": 500, "seasonNumber": season, "number": number,
			"absoluteNumber": absoluteNumber, "name": name, "overview": name + " overview",
			"aired": "2001-01-01", "runtime": 24, "image": "/banners/episodes/500/" + name + ".jpg",
		}
	}
	episodesBySeasonType := map[string][]map[string]any{
		"official": {
			episode(900, 0, 1, 0, "Special"),
			episode(901, 1, 1, 1, "Pilot"),
			episode(902, 1, 2, 2, "Second"),
			episode(903, 1, 3, 3, "Third"),
			episode(904, 2, 1, 4, "Fourth"),
			episode(905, 2, 2, 5, "Fifth"),
		},
		"dvd": {
			episode(902, 1, 1, 2, "Second"),
			episode(901, 1, 2, 1, "Pilot"),
			episode(903, 1, 3, 3, "Third"),
			episode(904, 1, 4, 4, "Fourth"),
			episode(905, 1, 5, 5, "Fifth"),
		},
		"absolute": {
			episode(901, 1, 1, 1, "Pilot"),
			episode(902, 1, 2, 2, "Second"),
			episode(903, 1, 3, 3, "Third"),
			episode(904, 1, 4, 4, "Fourth"),
			episode(905, 1, 5, 5, "Fifth"),
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && requests != nil {
			if counter, ok := requests[r.URL.Path]; ok {
				counter.Add(1)
			}
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "data": map[string]any{"token": "test-token"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/series/500/extended":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":                500,
					"name":              "Orders Show",
					"originalLanguage":  "eng",
					"defaultSeasonType": 1,
					"seasons": []map[string]any{
						{"id": 10, "seriesId": 500, "number": 0, "type": official},
						{"id": 11, "seriesId": 500, "number": 1, "type": official},
						{"id": 12, "seriesId": 500, "number": 2, "type": official},
						{"id": 21, "seriesId": 500, "number": 1, "type": dvd},
						{"id": 31, "seriesId": 500, "number": 1, "type": absolute},
					},
					"seasonTypes": []map[string]any{official, dvd, absolute, alternate},
				},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/series/500/episodes/"):
			episodes, ok := episodesBySeasonType[strings.TrimPrefix(r.URL.Path, "/series/500/episodes/")]
			if !ok {
				t.Errorf("unexpected season type request: %s", r.URL.String())
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"series":   map[string]any{"id": 500, "originalLanguage": "eng"},
					"episodes": episodes,
				},
				"links": map[string]any{"next": nil},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
}

func newEpisodeOrdersTestProvider(t *testing.T, requests map[string]*atomic.Int32) *Provider {
	t.Helper()
	server := newEpisodeOrdersTestServer(t, requests)
	t.Cleanup(server.Close)
	client := NewClient(1000)
	client.SetBaseURL(server.URL)
	return NewProviderWithClient(client)
}

func TestListEpisodeOrders_OneOrderPerSeasonTypeWithSeasons(t *testing.T) {
	t.Parallel()
	p := newEpisodeOrdersTestProvider(t, nil)

	orders, err := p.ListEpisodeOrders(context.Background(), metadata.EpisodeOrdersRequest{
		ProviderIDs: map[string]string{"tvdb": "500"},
		Language:    "en",
	})
	if err != nil {
		t.Fatalf("ListEpisodeOrders() error = %v", err)
	}

	// Alternate is in seasonTypes but has no seasons, so it is not offered.
	// Season counts leave out Specials, like the series' season count.
	want := []metadata.EpisodeOrder{
		{ID: "official", Name: "Aired Order", Kind: "aired", SeasonCount: 2, IsDefault: true},
		{ID: "dvd", Name: "DVD Order", Kind: "dvd", SeasonCount: 1},
		{ID: "absolute", Name: "Absolute Order", Kind: "absolute", SeasonCount: 1},
	}
	if !reflect.DeepEqual(orders, want) {
		t.Fatalf("orders = %+v\nwant %+v", orders, want)
	}
}

// orNone reads an optional episode number; -1 stands for "not set".
func orNone(v *int) int {
	if v == nil {
		return -1
	}
	return *v
}

func TestGetEpisodes_DVDOrderingKeepsAiredCoordinates(t *testing.T) {
	t.Parallel()
	p := newEpisodeOrdersTestProvider(t, nil)

	episodes, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
		ProviderIDs:  map[string]string{"tvdb": "500"},
		SeasonNumber: 1,
		Language:     "en",
		Ordering:     "dvd",
	})
	if err != nil {
		t.Fatalf("GetEpisodes() error = %v", err)
	}

	type coords struct {
		id                             string
		season, number                 int
		absolute, defSeason, defNumber int
	}
	got := make([]coords, len(episodes))
	for i, ep := range episodes {
		if ep.ProviderIDs["tvdb"] != ep.ContentID {
			t.Errorf("episode %d: ContentID %q, provider ids %v", i, ep.ContentID, ep.ProviderIDs)
		}
		got[i] = coords{ep.ContentID, ep.SeasonNumber, ep.EpisodeNumber, orNone(ep.AbsoluteNumber), orNone(ep.DefaultSeasonNumber), orNone(ep.DefaultEpisodeNumber)}
	}
	// The DVD release swaps the first two aired episodes and folds aired
	// season 2 into its season 1.
	want := []coords{
		{"902", 1, 1, 2, 1, 2},
		{"901", 1, 2, 1, 1, 1},
		{"903", 1, 3, 3, 1, 3},
		{"904", 1, 4, 4, 2, 1},
		{"905", 1, 5, 5, 2, 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("episodes:\n got %+v\nwant %+v", got, want)
	}
	if episodes[0].Title != "Second" || episodes[0].StillPath == "" {
		t.Errorf("episode fields not mapped: %+v", episodes[0])
	}
}

func TestGetEpisodes_DefaultOrderingAddsOnlyAbsoluteNumbers(t *testing.T) {
	t.Parallel()
	requests := map[string]*atomic.Int32{
		"/series/500/extended":          {},
		"/series/500/episodes/official": {},
	}
	p := newEpisodeOrdersTestProvider(t, requests)

	for _, tc := range []struct {
		season int
		want   map[string]int // episode ID -> absolute number, -1 for none
	}{
		{season: 0, want: map[string]int{"900": -1}},
		{season: 1, want: map[string]int{"901": 1, "902": 2, "903": 3}},
	} {
		episodes, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
			ProviderIDs:  map[string]string{"tvdb": "500"},
			SeasonNumber: tc.season,
			Language:     "en",
		})
		if err != nil {
			t.Fatalf("season %d: GetEpisodes() error = %v", tc.season, err)
		}
		got := make(map[string]int, len(episodes))
		for _, ep := range episodes {
			got[ep.ContentID] = orNone(ep.AbsoluteNumber)
			if ep.DefaultSeasonNumber != nil || ep.DefaultEpisodeNumber != nil {
				t.Errorf("season %d episode %s: default coordinates set in the default order", tc.season, ep.ContentID)
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("season %d: absolute numbers = %v, want %v", tc.season, got, tc.want)
		}
	}

	// The default order needs no series lookup, and one aired-order fetch
	// serves every season.
	if n := requests["/series/500/extended"].Load(); n != 0 {
		t.Errorf("series extended fetched %d times, want 0", n)
	}
	if n := requests["/series/500/episodes/official"].Load(); n != 1 {
		t.Errorf("aired episodes fetched %d times, want 1", n)
	}
}

func TestEpisodeOrdering_UnknownOrderingIsNotFound(t *testing.T) {
	t.Parallel()
	p := newEpisodeOrdersTestProvider(t, nil)
	ids := map[string]string{"tvdb": "500"}

	// "alternate" is a TVDB season type this series lists without seasons.
	for _, ordering := range []string{"alternate", "no-such-order"} {
		if _, err := p.GetSeasons(context.Background(), metadata.SeasonsRequest{ProviderIDs: ids, Ordering: ordering}); !errors.Is(err, ErrUnknownEpisodeOrder) {
			t.Errorf("GetSeasons(%q) error = %v, want ErrUnknownEpisodeOrder", ordering, err)
		}
		if _, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{ProviderIDs: ids, SeasonNumber: 1, Ordering: ordering}); !errors.Is(err, ErrUnknownEpisodeOrder) {
			t.Errorf("GetEpisodes(%q) error = %v, want ErrUnknownEpisodeOrder", ordering, err)
		}
	}
}

func TestGetSeasons_ServesTheOrderingsSeasons(t *testing.T) {
	t.Parallel()
	p := newEpisodeOrdersTestProvider(t, nil)

	for _, tc := range []struct {
		ordering string
		want     []int
	}{
		{ordering: "", want: []int{0, 1, 2}},
		{ordering: "dvd", want: []int{1}},
	} {
		seasons, err := p.GetSeasons(context.Background(), metadata.SeasonsRequest{
			ProviderIDs: map[string]string{"tvdb": "500"},
			Ordering:    tc.ordering,
		})
		if err != nil {
			t.Fatalf("GetSeasons(%q) error = %v", tc.ordering, err)
		}
		got := make([]int, len(seasons))
		for i, season := range seasons {
			got[i] = season.SeasonNumber
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("GetSeasons(%q) seasons = %v, want %v", tc.ordering, got, tc.want)
		}
	}
}

// A series can name a season type its own way (TVDB's alternateName, such
// as "Netflix Order"); that name is the one an admin recognizes.
func TestSeasonTypeNamePrefersTheSeriesName(t *testing.T) {
	t.Parallel()
	netflix := "Netflix Order"
	known := []SeasonType{{ID: 4, Type: "alternate", Name: "Alternate Order", AlternateName: &netflix}}
	cases := []struct {
		name       string
		seasonType SeasonType
		want       string
	}{
		{"series name from seasonTypes", SeasonType{ID: 4}, "Netflix Order"},
		{"series name on the season", SeasonType{ID: 5, Name: "Alternate DVD Order", AlternateName: &netflix}, "Netflix Order"},
		{"generic name", SeasonType{ID: 2, Name: "DVD Order"}, "DVD Order"},
		{"slug", SeasonType{ID: 9}, "regional"},
	}
	for _, tc := range cases {
		if got := seasonTypeName(tc.seasonType, known, "regional"); got != tc.want {
			t.Errorf("%s: seasonTypeName = %q, want %q", tc.name, got, tc.want)
		}
	}
}
