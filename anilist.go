package anilistgo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	BaseAPIURL       = "https://graphql.anilist.co"
	AnimeURLFormat   = "https://anilist.co/anime/%d"
	AnilistURLFormat = "https://anilist.co/%s/%d"
	PerPage          = 20
	MediaTypeAnime   = "ANIME"
	MediaTypeManga   = "MANGA"

	AnimeSearchQueryWithSeason = `
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

	AnimeSearchQuery = `
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

	UserQuery = `
    query ($name: String) {
        User (name: $name) {
            id
        }
    }
    `

	FollowingQuery = `
    query ($id: Int!, $page: Int, $perPage: Int) {
      Page (page: $page, perPage: $perPage) {
        pageInfo {
          hasNextPage
        }
        users: following(userId: $id) {
          name
        }
      }
    }
    `

	UpdatesQuery = `
    query ($userName: String, $type: MediaType) {
		MediaListCollection(userName: $userName, type: $type) {
			lists {
				entries {
					mediaId
					media {
						title {
							english
							romaji
						}
                        coverImage {
                            extraLarge
                        }
						episodes
						chapters
						volumes
					}
					score (format: POINT_100)
					progress
					progressVolumes
					status
					updatedAt
				}
			}
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
		MediaData           Media                `json:"Media"`
		MediaListCollection *MediaListCollection `json:"MediaListCollection"`
		User                UserInfo             `json:"User,omitempty"`
		Page                *PageData            `json:"Page,omitempty"`
		Errors              []struct {
			Message string `json:"message"`
			Status  int    `json:"status"`
		} `json:"errors,omitempty"`
	} `json:"data"`
}

type MediaListCollection struct {
	Lists []struct {
		Entries []struct {
			MediaID         int    `json:"mediaId"`
			Score           int    `json:"score"`
			Progress        *int   `json:"progress"`
			ProgressVolumes *int   `json:"progressVolumes"`
			Status          string `json:"status"`
			UpdatedAt       int64  `json:"updatedAt"`
			Media           struct {
				Title struct {
					English string `json:"english"`
					Romaji  string `json:"romaji"`
				} `json:"title"`
				CoverImage struct {
					ExtraLarge string `json:"extraLarge"`
				}
				Episodes *int `json:"episodes"`
				Chapters *int `json:"chapters"`
				Volumes  *int `json:"volumes"`
			} `json:"media"`
		} `json:"entries"`
	} `json:"lists"`
}

type UserInfo struct {
	ID int `json:"id"`
}

type PageData struct {
	PageInfo struct {
		HasNextPage bool `json:"hasNextPage"`
	} `json:"pageInfo"`
	Users []struct {
		Name string `json:"name"`
	} `json:"users"`
}

type Update struct {
	UserName      string
	MediaID       int
	Title         string
	URL           string
	CoverURL      string
	Status        string
	UpdatedTime   int64
	Score         int
	Progress      *int
	ProgressVol   *int
	TotalEpisodes *int
	TotalVolumes  *int
	TotalChapters *int
	MediaType     string
}

type AnilistItem struct {
	AnilistURL   string
	AnilistScore int
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
		query = AnimeSearchQueryWithSeason
		variables = map[string]interface{}{
			"title":      title,
			"season":     season,
			"seasonYear": seasonYear,
		}
	} else {
		query = AnimeSearchQuery
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

// GetFollowingNames retrieves the names of users that the provided user is following on Anilist.
// The function first fetches the user ID associated with the given username and then uses that ID
// to get the list of following users.
//
// Parameters:
// - username: The username of the user for whom you want to fetch the list of followed users.
//
// Returns:
// - A slice of strings, where each string is the name of a user that the provided user is following.
// - An error if there's any issue fetching the data. If no error is returned, the function was successful.
func GetFollowingNames(username string) ([]string, error) {
	variables := map[string]interface{}{
		"name": username,
	}

	userID, err := fetchUserID(UserQuery, variables)
	if err != nil {
		return nil, err
	}

	var page = 1
	var names []string
	var hasNextPage = true

	for hasNextPage {
		variables = map[string]interface{}{
			"id":      userID,
			"page":    page,
			"perPage": PerPage,
		}

		pageData, err := fetchFollowingData(FollowingQuery, variables)
		if err != nil {
			return nil, err
		}

		for _, user := range pageData.Users {
			names = append(names, user.Name)
		}

		hasNextPage = pageData.PageInfo.HasNextPage
		page++
	}

	return names, nil
}

// GetUpdates retrieves a list of media updates for a specified user on Anilist.
// The media updates can be of type MediaTypeAnime ("ANIME") or MediaTypeManga ("MANGA").
// Each update provides information such as the media title, its URL, status, last updated time,
// score, and progress details like episodes watched or chapters/volumes read.
//
// Parameters:
//   - username: The username of the user for whom you want to fetch the media updates.
//   - mediaType: Specifies the type of media updates to fetch.
//     Accepts constants MediaTypeAnime or MediaTypeManga.
//
// Returns:
//   - A slice of Update structs, each representing an individual media update for the user.
//   - An error if there's any issue fetching the data or if the provided mediaType is invalid.
//     If no error is returned, the function was successful.
//
// Constants:
// - MediaTypeAnime: Represents the "ANIME" type of media.
// - MediaTypeManga: Represents the "MANGA" type of media.
func GetUpdates(username string, mediaType string) ([]Update, error) {
	// Check if the provided mediaType is valid
	if mediaType != MediaTypeAnime && mediaType != MediaTypeManga {
		return nil, fmt.Errorf("invalid mediaType provided: %s. Accepts only %s or %s", mediaType, MediaTypeAnime, MediaTypeManga)
	}
	var updates []Update

	variables := map[string]interface{}{
		"userName": username,
		"type":     mediaType,
	}

	mediaListCollection, err := fetchUpdatesData(UpdatesQuery, variables)
	if err != nil {
		return nil, err
	}

	for _, mediaList := range mediaListCollection.Lists {
		for _, entry := range mediaList.Entries {
			update := Update{
				UserName:    username,
				MediaID:     entry.MediaID,
				Title:       entry.Media.Title.English,
				URL:         fmt.Sprintf(AnilistURLFormat, strings.ToLower(mediaType), entry.MediaID),
				CoverURL:    entry.Media.CoverImage.ExtraLarge,
				Status:      entry.Status,
				UpdatedTime: entry.UpdatedAt,
				Score:       entry.Score,
				MediaType:   mediaType,
			}

			if update.Title == "" {
				update.Title = entry.Media.Title.Romaji
			}

			if mediaType == MediaTypeAnime {
				update.Progress = entry.Progress
				update.TotalEpisodes = entry.Media.Episodes
			} else if mediaType == MediaTypeManga {
				update.Progress = entry.Progress
				update.ProgressVol = entry.ProgressVolumes
				update.TotalVolumes = entry.Media.Volumes
				update.TotalChapters = entry.Media.Chapters
			}

			updates = append(updates, update)
		}
	}

	return updates, nil
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

func fetchUserID(query string, variables map[string]interface{}) (int, error) {
	data, err := sendRequest(BaseAPIURL, query, variables)
	if err != nil {
		return 0, err
	}

	return data.Data.User.ID, nil
}

func fetchFollowingData(query string, variables map[string]interface{}) (*PageData, error) {
	data, err := sendRequest(BaseAPIURL, query, variables)
	if err != nil {
		return nil, err
	}

	return data.Data.Page, nil
}

func fetchUpdatesData(query string, variables map[string]interface{}) (*MediaListCollection, error) {
	data, err := sendRequest(BaseAPIURL, query, variables)
	if err != nil {
		return nil, err
	}

	return data.Data.MediaListCollection, nil
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
