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
			coverImage {
				extraLarge
			}
			episodes
			chapters
			volumes
            averageScore
        }
    }
    `

	AnimeSearchQueryByID = `
    query ($id: Int) {
        Media (id: $id) {
            id
            title {
                romaji
                english
                native
            }
			coverImage {
				extraLarge
			}
			episodes
			chapters
			volumes
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
			coverImage {
				extraLarge
			}
			episodes
			chapters
			volumes
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

	ProgressQuery = `
    query ($userName: String, $mediaId: Int) {
      MediaList (userName: $userName, mediaId: $mediaId) {
        progress
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

	UpdateProgressQuery = `
    mutation ($mediaId: Int, $progress: Int, $status: MediaListStatus) {
      SaveMediaListEntry (mediaId: $mediaId, progress: $progress, status: $status) {
        id
        progress
        status
      }
    }
    `
)

var (
	AnimeSeasons          = []string{"WINTER", "SPRING", "SUMMER", "FALL", "WINTER"}
	BeginningSeasonMonths = []int{1, 4, 7, 10}
	EndSeasonMonths       = []int{3, 6, 9, 12}
)

type AuthenticatedAPI struct {
	AccessToken string
}

type MediaTitle struct {
	Romaji  string `json:"romaji"`
	English string `json:"english"`
	Native  string `json:"native"`
}

type Media struct {
	ID           int        `json:"id"`
	AverageScore int        `json:"averageScore"`
	Title        MediaTitle `json:"title"`
	CoverImage   struct {
		ExtraLarge string `json:"extraLarge"`
	}
	Episodes *int `json:"episodes"`
	Chapters *int `json:"chapters"`
	Volumes  *int `json:"volumes"`
}

type Response struct {
	Data struct {
		MediaData           Media                `json:"Media"`
		MediaList           MediaList            `json:"MediaList"`
		MediaListCollection *MediaListCollection `json:"MediaListCollection"`
		User                UserInfo             `json:"User,omitempty"`
		Page                *PageData            `json:"Page,omitempty"`
		Errors              []struct {
			Message string `json:"message"`
			Status  int    `json:"status"`
		} `json:"errors,omitempty"`
	} `json:"data"`
}

type MediaList struct {
	Progress int `json:"progress"`
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
			Media           Media  `json:"media"`
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
	ID       int
	URL      string
	Score    int
	Episodes *int
}

// NewAuthenticatedAPI creates and returns a new instance of AuthenticatedAPI
// configured with the provided access token. The returned API object is
// capable of making authenticated requests to the AniList API, allowing it to
// interact with user-specific data, such as updating a user's media progress.
//
// Parameters:
//   - accessToken: A string that contains the OAuth2 access token obtained
//     from AniList. The access token is used to authenticate
//     requests made to the API.
//
// The function returns a pointer to an AuthenticatedAPI instance, configured
// with the provided access token. This instance should be used to make
// authenticated API requests on behalf of the user.
//
// Usage:
//
//	api := NewAuthenticatedAPI("your_access_token")
func NewAuthenticatedAPI(accessToken string) *AuthenticatedAPI {
	return &AuthenticatedAPI{
		AccessToken: accessToken,
	}
}

// GetAnilistItemByID retrieves the Anilist URL and average score for a given anime ID.
// The function returns an AnilistItem containing the URL, score, and other relevant data.
// If no matching anime is found, an empty AnilistItem and potentially an error are returned.
//
// id: The ID of the anime to search for.
//
// Returns:
// - AnilistItem: A struct containing the Anilist URL, score, and other data for the found anime.
// - error: Any errors encountered during the search.
func GetAnilistItemByID(id int) (AnilistItem, error) {
	variables := map[string]interface{}{
		"id": id,
	}

	media, err := fetchAnilistData(AnimeSearchQueryByID, variables)
	if err != nil {
		return AnilistItem{}, err
	}

	if media.ID != 0 {
		url := fmt.Sprintf(AnimeURLFormat, media.ID)
		score := media.AverageScore
		return AnilistItem{
			ID:       media.ID,
			URL:      url,
			Score:    score,
			Episodes: media.Episodes,
		}, nil
	}

	return AnilistItem{}, nil
}

// FindAnilistItem retrieves the Anilist URL and average score for a given anime title.
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
func FindAnilistItem(title string, firstEpisodeDate *time.Time, offset int) (AnilistItem, error) {
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
			ID:       media.ID,
			URL:      url,
			Score:    score,
			Episodes: media.Episodes,
		}, nil
	} else if firstEpisodeDate != nil && isMonthInList(*firstEpisodeDate, BeginningSeasonMonths) && offset == 0 {
		return FindAnilistItem(title, firstEpisodeDate, -1)
	} else if firstEpisodeDate != nil && isMonthInList(*firstEpisodeDate, EndSeasonMonths) && offset == 0 {
		return FindAnilistItem(title, firstEpisodeDate, 1)
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

// UpdateProgress updates the progress status of a media item on AniList for
// the authenticated user.
//
// This method performs a GraphQL mutation by making a HTTP POST request to
// the AniList API, updating the user's progress on a specific media item.
// Progress and status update can be used for marking media as watched/read,
// or updating the watching/reading progress of a media item.
//
// Parameters:
//   - mediaID: An integer that uniquely identifies the media item on AniList.
//   - progress: An integer representing the progress the user has made
//     with the media item. For series, it is typically the number
//     of watched episodes or read chapters.
//   - status: A string indicating the user's watching/reading status for
//     the media item. This should be a value from the predefined
//     set of status strings defined by the AniList API, such as
//     "CURRENT", "PLANNING", "COMPLETED", etc.
//
// The method will return an error if the request to the API fails, which
// could be due to a variety of reasons: network issues, invalid access token,
// invalid mediaID, or API changes. Otherwise, it returns nil indicating that
// the progress update was successful.
//
// Usage:
//
//	api := &AuthenticatedAPI{
//	    AccessToken: "your_access_token",
//	}
//	err := api.UpdateProgress(12345, 7, "CURRENT")
//	if err != nil {
//	    log.Fatal(err)
//	}
func (api *AuthenticatedAPI) UpdateProgress(mediaID int, progress int, status string) error {
	variables := map[string]interface{}{
		"mediaId":  mediaID,
		"progress": progress,
		"status":   status,
	}

	_, err := sendRequest(BaseAPIURL, UpdateProgressQuery, variables, api.AccessToken)
	if err != nil {
		return err
	}
	return nil
}

// GetProgress retrieves the watching progress of a specific media item for a user
// from the Anilist API. It queries the Anilist API for the progress of a media item,
// identified by its mediaID, for a specific user, identified by their userName.
//
// Parameters:
// - userName: A string representing the Anilist user's name whose progress is being fetched.
// - mediaID: An integer representing the unique identifier of the media item on Anilist.
//
// Returns:
//   - An integer representing the progress of the media item for the user. The progress
//     is returned as the number of episodes watched. If the progress cannot be fetched
//     (due to user not watching the media, mediaID not existing, or other reasons),
//     it returns 0.
//   - An error which can occur during the API request, JSON parsing, or other stages.
//     Returns nil if the function runs successfully.
//
// Example usage:
//
//	progress, err := GetProgress("exampleUser", 12345)
//	if err != nil {
//	    fmt.Printf("An error occurred: %v\n", err)
//	    return
//	}
//	fmt.Printf("The progress for mediaID 12345 for exampleUser is: %d\n", progress)
func GetProgress(userName string, mediaID int) (int, error) {
	variables := map[string]interface{}{
		"mediaId":  mediaID,
		"userName": userName,
	}

	progress, err := fetchProgress(ProgressQuery, variables)
	if err != nil {
		return 0, err
	}
	return progress, nil
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
	data, err := sendRequest(BaseAPIURL, query, variables, "")
	if err != nil {
		return Media{}, err
	}
	return data.Data.MediaData, nil
}

func fetchProgress(query string, variables map[string]interface{}) (int, error) {
	data, err := sendRequest(BaseAPIURL, query, variables, "")
	if err != nil {
		return 0, err
	}
	return data.Data.MediaList.Progress, nil
}

func fetchUserID(query string, variables map[string]interface{}) (int, error) {
	data, err := sendRequest(BaseAPIURL, query, variables, "")
	if err != nil {
		return 0, err
	}

	return data.Data.User.ID, nil
}

func fetchFollowingData(query string, variables map[string]interface{}) (*PageData, error) {
	data, err := sendRequest(BaseAPIURL, query, variables, "")
	if err != nil {
		return nil, err
	}

	return data.Data.Page, nil
}

func fetchUpdatesData(query string, variables map[string]interface{}) (*MediaListCollection, error) {
	data, err := sendRequest(BaseAPIURL, query, variables, "")
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

func sendRequest(url, query string, variables map[string]interface{}, accessToken string) (*Response, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			panic(err)
		}
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode > http.StatusIMUsed {
		return nil, fmt.Errorf("request failed with status code %d: %s", resp.StatusCode, string(body))
	}

	var result Response
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}
