package anilistgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
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
	Timeout          = 30
	MaxErrorBodySize = 500

	// MaxRetries is how many extra attempts a request gets after AniList
	// transiently refuses it.
	MaxRetries = 2
	// RetryBaseDelay is the wait before the first retry; it doubles thereafter.
	RetryBaseDelay = 500 * time.Millisecond
	// MaxRetryDelay caps how long a single retry waits, keeping a library call
	// short enough that the caller stays in charge of longer waits.
	MaxRetryDelay = 5 * time.Second
	// RetryJitter is the fraction by which a retry wait is randomly lengthened.
	RetryJitter = 0.3

	AnimeSearchQueryWithSeason = `
    query ($title: String, $season: MediaSeason, $seasonYear: Int) {
        Media (type: ANIME, search: $title, season: $season, seasonYear: $seasonYear, format_not_in: [MOVIE, MUSIC]) {
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
        Media (type: ANIME, search: $title, format_not_in: [MOVIE, MUSIC]) {
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

	LimitedUpdatesQuery = `
    query ($userName: String, $type: MediaType, $chunk: Int, $perChunk: Int) {
        MediaListCollection(userName: $userName, type: $type, chunk: $chunk, perChunk: $perChunk, sort: UPDATED_TIME_DESC) {
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
	baseAPIURL            = BaseAPIURL
	httpClient            = &http.Client{Timeout: time.Second * Timeout}
)

type AuthenticatedAPI struct {
	AccessToken string
}

// APIError describes a failed AniList API call. It is returned whenever AniList
// answers with a non-2xx status, or with a GraphQL errors array that carries a
// status of its own. Callers can reach it with errors.As to react to specific
// conditions, most usefully rate limiting:
//
//	var apiErr *anilistgo.APIError
//	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
//	    time.Sleep(apiErr.RetryAfter)
//	}
type APIError struct {
	// StatusCode is the status AniList reported: the HTTP status of a failed
	// request, or the status carried in a GraphQL error when the HTTP request
	// itself succeeded.
	StatusCode int

	// RetryAfter is how long AniList asked the caller to wait, taken from the
	// Retry-After header. It is zero when AniList sent no such hint, which is
	// the case for the 429s served by AniList's edge rather than its API.
	RetryAfter time.Duration

	// Limit and Remaining mirror the X-RateLimit-Limit and X-RateLimit-Remaining
	// headers. Both are -1 when AniList omitted the header.
	Limit     int
	Remaining int

	// Body is the response body, truncated to MaxErrorBodySize.
	Body string

	// GraphQLMessages holds the messages from a GraphQL errors array, if the
	// response carried one.
	GraphQLMessages []string

	// FromGraphQL reports whether this error was raised by a GraphQL errors
	// array on an otherwise successful HTTP response.
	FromGraphQL bool
}

func (e *APIError) Error() string {
	if e.FromGraphQL {
		return "anilist graphql error: " + strings.Join(e.GraphQLMessages, "; ")
	}

	parts := []string{fmt.Sprintf("anilist request failed: status %d", e.StatusCode)}
	if e.Limit >= 0 {
		parts = append(parts, fmt.Sprintf("X-RateLimit-Limit=%d", e.Limit))
	}
	if e.Remaining >= 0 {
		parts = append(parts, fmt.Sprintf("X-RateLimit-Remaining=%d", e.Remaining))
	}
	if e.RetryAfter > 0 {
		parts = append(parts, fmt.Sprintf("Retry-After=%d", int(e.RetryAfter.Seconds())))
	}
	if len(e.GraphQLMessages) > 0 {
		parts = append(parts, "graphql="+strings.Join(e.GraphQLMessages, "; "))
	}
	if e.Body != "" {
		parts = append(parts, "body="+e.Body)
	}

	return strings.Join(parts, "; ")
}

type GraphQLError struct {
	Message    string `json:"message"`
	Status     int    `json:"status"`
	Extensions struct {
		Status int `json:"status"`
	} `json:"extensions"`
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
	} `json:"data"`
	Errors []GraphQLError `json:"errors,omitempty"`
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
	return GetAnilistItemByIDContext(context.Background(), id)
}

// GetAnilistItemByIDContext retrieves an AniList item by ID using ctx for
// request cancellation and deadlines.
func GetAnilistItemByIDContext(ctx context.Context, id int) (AnilistItem, error) {
	variables := map[string]interface{}{
		"id": id,
	}

	media, err := fetchAnilistDataContext(ctx, AnimeSearchQueryByID, variables)
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
// in which the anime aired to refine the search. Movies and music videos are excluded, since a
// franchise film releasing in the same season can otherwise outrank the TV series it belongs to.
// The function returns an AnilistItem containing
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
	return FindAnilistItemContext(context.Background(), title, firstEpisodeDate, offset)
}

// FindAnilistItemContext finds an AniList item like FindAnilistItem, using ctx
// for request cancellation and deadlines.
func FindAnilistItemContext(ctx context.Context, title string, firstEpisodeDate *time.Time, offset int) (AnilistItem, error) {
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

	media, err := fetchAnilistDataContext(ctx, query, variables)
	if err != nil && !isNotFound(err) {
		return AnilistItem{}, err
	}

	// AniList answers a search without a match with 404. That is a miss, not a
	// failure: it must reach the adjacent-season retry below, and the caller must
	// see an empty item rather than an error.
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
		return FindAnilistItemContext(ctx, title, firstEpisodeDate, -1)
	} else if firstEpisodeDate != nil && isMonthInList(*firstEpisodeDate, EndSeasonMonths) && offset == 0 {
		return FindAnilistItemContext(ctx, title, firstEpisodeDate, 1)
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
	return GetFollowingNamesContext(context.Background(), username)
}

// GetFollowingNamesContext retrieves followed usernames like GetFollowingNames,
// using ctx for request cancellation and deadlines.
func GetFollowingNamesContext(ctx context.Context, username string) ([]string, error) {
	variables := map[string]interface{}{
		"name": username,
	}

	userID, err := fetchUserIDContext(ctx, UserQuery, variables)
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

		pageData, err := fetchFollowingDataContext(ctx, FollowingQuery, variables)
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
func GetUpdates(username string, mediaType string, chunk *int, perChunk *int) ([]Update, error) {
	return GetUpdatesContext(context.Background(), username, mediaType, chunk, perChunk)
}

// GetUpdatesContext retrieves media updates like GetUpdates, using ctx for
// request cancellation and deadlines.
func GetUpdatesContext(ctx context.Context, username string, mediaType string, chunk *int, perChunk *int) ([]Update, error) {
	// Check if the provided mediaType is valid
	if mediaType != MediaTypeAnime && mediaType != MediaTypeManga {
		return nil, fmt.Errorf("invalid mediaType provided: %s. Accepts only %s or %s", mediaType, MediaTypeAnime, MediaTypeManga)
	}
	var updates []Update

	variables := map[string]interface{}{
		"userName": username,
		"type":     mediaType,
	}

	var query = UpdatesQuery
	// Only add chunk and perChunk to variables if they are not nil
	if chunk != nil && perChunk != nil {
		variables["chunk"] = *chunk
		variables["perChunk"] = *perChunk
		query = LimitedUpdatesQuery
	}

	mediaListCollection, err := fetchUpdatesDataContext(ctx, query, variables)
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
	return api.UpdateProgressContext(context.Background(), mediaID, progress, status)
}

// UpdateProgressContext updates progress like UpdateProgress, using ctx for
// request cancellation and deadlines.
func (api *AuthenticatedAPI) UpdateProgressContext(ctx context.Context, mediaID int, progress int, status string) error {
	variables := map[string]interface{}{
		"mediaId":  mediaID,
		"progress": progress,
		"status":   status,
	}

	_, err := sendRequestContext(ctx, baseAPIURL, UpdateProgressQuery, variables, api.AccessToken)
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
	return GetProgressContext(context.Background(), userName, mediaID)
}

// GetProgressContext retrieves progress like GetProgress, using ctx for request
// cancellation and deadlines.
func GetProgressContext(ctx context.Context, userName string, mediaID int) (int, error) {
	variables := map[string]interface{}{
		"mediaId":  mediaID,
		"userName": userName,
	}

	progress, err := fetchProgressContext(ctx, ProgressQuery, variables)
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
	return fetchAnilistDataContext(context.Background(), query, variables)
}

func fetchAnilistDataContext(ctx context.Context, query string, variables map[string]interface{}) (Media, error) {
	data, err := sendRequestContext(ctx, baseAPIURL, query, variables, "")
	if err != nil {
		return Media{}, err
	}
	return data.Data.MediaData, nil
}

func fetchProgress(query string, variables map[string]interface{}) (int, error) {
	return fetchProgressContext(context.Background(), query, variables)
}

func fetchProgressContext(ctx context.Context, query string, variables map[string]interface{}) (int, error) {
	data, err := sendRequestContext(ctx, baseAPIURL, query, variables, "")
	if err != nil {
		return 0, err
	}
	return data.Data.MediaList.Progress, nil
}

func fetchUserID(query string, variables map[string]interface{}) (int, error) {
	return fetchUserIDContext(context.Background(), query, variables)
}

func fetchUserIDContext(ctx context.Context, query string, variables map[string]interface{}) (int, error) {
	data, err := sendRequestContext(ctx, baseAPIURL, query, variables, "")
	if err != nil {
		return 0, err
	}
	if data.Data.User.ID == 0 {
		return 0, errors.New("anilist response did not include user data")
	}

	return data.Data.User.ID, nil
}

func fetchFollowingData(query string, variables map[string]interface{}) (*PageData, error) {
	return fetchFollowingDataContext(context.Background(), query, variables)
}

func fetchFollowingDataContext(ctx context.Context, query string, variables map[string]interface{}) (*PageData, error) {
	data, err := sendRequestContext(ctx, baseAPIURL, query, variables, "")
	if err != nil {
		return nil, err
	}
	if data.Data.Page == nil {
		return nil, errors.New("anilist response did not include following page data")
	}

	return data.Data.Page, nil
}

func fetchUpdatesData(query string, variables map[string]interface{}) (*MediaListCollection, error) {
	return fetchUpdatesDataContext(context.Background(), query, variables)
}

func fetchUpdatesDataContext(ctx context.Context, query string, variables map[string]interface{}) (*MediaListCollection, error) {
	data, err := sendRequestContext(ctx, baseAPIURL, query, variables, "")
	if err != nil {
		return nil, err
	}
	if data.Data.MediaListCollection == nil {
		return nil, errors.New("anilist response did not include media list collection")
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
	return sendRequestContext(context.Background(), url, query, variables, accessToken)
}

func sendRequestContext(ctx context.Context, url, query string, variables map[string]interface{}, accessToken string) (*Response, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return nil, err
	}

	for attempt := 0; ; attempt++ {
		result, err := doRequestContext(ctx, url, reqBody, accessToken)
		if err == nil {
			return result, nil
		}

		if attempt >= MaxRetries {
			return nil, err
		}

		delay, retryable := retryDelay(err, attempt)
		if !retryable {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func doRequestContext(ctx context.Context, url string, reqBody []byte, accessToken string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result Response
	unmarshalErr := json.Unmarshal(body, &result)

	// A failed status takes precedence over the GraphQL errors array: AniList
	// reports rate limiting through both, but only the headers carry Retry-After.
	if resp.StatusCode < http.StatusOK || resp.StatusCode > http.StatusIMUsed {
		apiError := newAPIError(resp, body)
		if unmarshalErr == nil && len(result.Errors) > 0 {
			apiError.GraphQLMessages = graphQLMessages(result.Errors)
		}
		return nil, apiError
	}

	if unmarshalErr != nil {
		return nil, unmarshalErr
	}

	if len(result.Errors) > 0 {
		return nil, newGraphQLAPIError(result.Errors)
	}

	return &result, nil
}

// retryDelay reports how long to wait before retrying err, and whether retrying
// is worthwhile at all.
//
// Only AniList's own transient refusals are retried. Its edge rejects a share of
// otherwise valid requests, so a request that just failed is quite likely to
// succeed moments later; without this, a caller that issues several requests per
// cycle sees a per-request failure rate compound into a much higher cycle
// failure rate.
//
// A Retry-After longer than MaxRetryDelay is not slept through: waiting that
// long belongs to the caller, whose own backoff can afford it without stalling a
// library call.
func retryDelay(err error, attempt int) (time.Duration, bool) {
	var apiError *APIError
	if !errors.As(err, &apiError) {
		return 0, false
	}
	if apiError.StatusCode != http.StatusTooManyRequests && apiError.StatusCode < http.StatusInternalServerError {
		return 0, false
	}

	if apiError.RetryAfter > 0 {
		if apiError.RetryAfter > MaxRetryDelay {
			return 0, false
		}
		return apiError.RetryAfter, true
	}

	delay := min(RetryBaseDelay<<attempt, MaxRetryDelay)

	// Jitter upward, so clients refused in the same instant do not retry in
	// lockstep and collide again.
	return delay + time.Duration(rand.Float64()*RetryJitter*float64(delay)), true
}

// isNotFound reports whether err is AniList's 404, which it uses for "nothing
// matched" as well as for genuinely missing resources.
func isNotFound(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound
}

func newAPIError(resp *http.Response, body []byte) *APIError {
	bodyText := strings.TrimSpace(string(body))
	if len(bodyText) > MaxErrorBodySize {
		bodyText = bodyText[:MaxErrorBodySize] + "..."
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		Limit:      parseRateLimitHeader(resp.Header.Get("X-RateLimit-Limit")),
		Remaining:  parseRateLimitHeader(resp.Header.Get("X-RateLimit-Remaining")),
		Body:       bodyText,
	}
}

// parseRetryAfter interprets a Retry-After header, which RFC 9110 allows to be
// either a delay in seconds or an absolute HTTP date. It returns zero when the
// header is absent or unparseable.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	if deadline, err := http.ParseTime(value); err == nil {
		if delay := time.Until(deadline); delay > 0 {
			return delay
		}
	}

	return 0
}

// parseRateLimitHeader returns the header's integer value, or -1 when AniList
// omitted the header or sent something unparseable.
func parseRateLimitHeader(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return -1
	}
	return parsed
}

func graphQLMessages(graphQLErrors []GraphQLError) []string {
	messages := make([]string, 0, len(graphQLErrors))
	for _, graphQLError := range graphQLErrors {
		message := graphQLError.Message
		if status := graphQLErrorStatus(graphQLError); status != 0 {
			message = fmt.Sprintf("%s (status %d)", message, status)
		}
		messages = append(messages, message)
	}

	return messages
}

// graphQLErrorStatus returns the status AniList attached to a GraphQL error,
// which it reports either at the top level or under extensions.
func graphQLErrorStatus(graphQLError GraphQLError) int {
	if graphQLError.Status != 0 {
		return graphQLError.Status
	}
	return graphQLError.Extensions.Status
}

func newGraphQLAPIError(graphQLErrors []GraphQLError) *APIError {
	apiError := &APIError{
		Limit:           -1,
		Remaining:       -1,
		GraphQLMessages: graphQLMessages(graphQLErrors),
		FromGraphQL:     true,
	}

	// AniList signals conditions such as rate limiting through the GraphQL
	// payload even on a 200, so surface the first status it reports.
	for _, graphQLError := range graphQLErrors {
		if status := graphQLErrorStatus(graphQLError); status != 0 {
			apiError.StatusCode = status
			break
		}
	}

	return apiError
}
