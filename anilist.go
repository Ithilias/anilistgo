package anilistgo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	BaseAPIURL     = "https://graphql.anilist.co"
	AnimeURLFormat = "https://anilist.co/anime/%d"

	animeSearchQueryWithSeason = `
    query ($title: String, $season: MediaSeason, $seasonYear: Int) {
        Media (type: ANIME, search: $title, season: $season, seasonYear: $seasonYear) {
            id
            title {
                romaji
                english
                native
            }
            averageScore
        }
    }
    `

	animeSearchQuery = `
    query ($title: String) {
        Media (type: ANIME, search: $title) {
            id
            title {
                romaji
                english
                native
            }
            averageScore
        }
    }
    `
)

var (
	AnimeSeasons          = []string{"WINTER", "SPRING", "SUMMER", "FALL", "WINTER"}
	BeginningSeasonMonths = []int{1, 4, 7, 10}
	EndSeasonMonths       = []int{3, 6, 9, 12}
)

type MediaTitle struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
}

type Media struct {
	ID           int        `json:"id"`
	AverageScore int        `json:"averageScore"`
	Title        MediaTitle `json:"title"`
}

type Response struct {
	Data struct {
		MediaData Media `json:"Media"`
	} `json:"data"`
}

type AnilistItem struct {
	AnilistURL   string
	AnilistScore int
}

func computeSeason(firstEpisodeDate time.Time, offset int) (string, int) {
	seasonIndex := (int(firstEpisodeDate.Month())-1)/3 + offset
	seasonYear := firstEpisodeDate.Year()

	if seasonIndex < 0 {
		seasonIndex = 3
		seasonYear--
	} else if seasonIndex > 3 {
		seasonIndex = 0
		seasonYear++
	}

	return AnimeSeasons[seasonIndex], seasonYear
}

func fetchAnilistData(query string, variables map[string]interface{}) (Media, error) {
	data, err := sendRequest(BaseAPIURL, query, variables)
	if err != nil {
		return Media{}, err
	}
	return data.Data.MediaData, nil
}

// GetAnilistURLAndScore retrieves the Anilist URL and average score for a given anime title.
// If a date for the first episode is provided, the function will also consider the season
// in which the anime aired to refine the search. The function returns an AnilistItem containing
// the URL and score. If no matching anime is found, an empty AnilistItem and potentially an error
// are returned.
//
// title: The title of the anime to search for.
// firstEpisodeDate: Optional date of the first episode's airing. Used to refine the search by season.
// offset: Adjusts the season calculation based on the month of the firstEpisodeDate. Default: 0
//
// Returns:
// - AnilistItem: A struct containing the Anilist URL and score for the found anime.
// - error: Any errors encountered during the search.
func GetAnilistURLAndScore(title string, firstEpisodeDate *time.Time, offset int) (AnilistItem, error) {
	var query string
	var variables map[string]interface{}

	if firstEpisodeDate != nil {
		season, seasonYear := computeSeason(*firstEpisodeDate, offset)
		query = animeSearchQueryWithSeason
		variables = map[string]interface{}{
			"title":      title,
			"season":     season,
			"seasonYear": seasonYear,
		}
	} else {
		query = animeSearchQuery
		variables = map[string]interface{}{
			"title": title,
		}
	}

	media, err := fetchAnilistData(query, variables)
	if err != nil {
		return AnilistItem{}, err
	}

	if media.ID != 0 {
		url := fmt.Sprintf(AnimeURLFormat, media.ID)
		score := media.AverageScore
		return AnilistItem{
			AnilistURL:   url,
			AnilistScore: score,
		}, nil
	} else if firstEpisodeDate != nil && isMonthInList(*firstEpisodeDate, BeginningSeasonMonths) && offset == 0 {
		return GetAnilistURLAndScore(title, firstEpisodeDate, -1)
	} else if firstEpisodeDate != nil && isMonthInList(*firstEpisodeDate, EndSeasonMonths) && offset == 0 {
		return GetAnilistURLAndScore(title, firstEpisodeDate, 1)
	}

	return AnilistItem{}, nil
}

func isMonthInList(date time.Time, list []int) bool {
	for _, m := range list {
		if m == int(date.Month()) {
			return true
		}
	}
	return false
}

func sendRequest(url, query string, variables map[string]interface{}) (*Response, error) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err = Body.Close()
	}(resp.Body)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result Response
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}
