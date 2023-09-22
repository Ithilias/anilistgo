package anilistgo

import (
	"testing"
	"time"
)

func TestGetAnilistURLAndScore(t *testing.T) {
	firstEpisodeDateFirstSeasonAoT, _ := time.Parse("2006-01-02", "2013-04-07")
	firstEpisodeDateLastSeasonAoT, _ := time.Parse("2006-01-02", "2020-12-07")
	firstEpisodeDate21SeasonOnePiece, _ := time.Parse("2006-01-02", "2021-10-10")
	tests := []struct {
		title            string
		firstEpisodeDate *time.Time
		offset           int
		expectedURL      string
		expectedScore    int
		expectScore      bool
		expectError      bool
	}{
		{"Attack on Titan", &firstEpisodeDateFirstSeasonAoT, 0, "https://anilist.co/anime/16498", 0, false, false},
		{"Attack on Titan", &firstEpisodeDateLastSeasonAoT, 0, "https://anilist.co/anime/110277", 0, false, false},
		{"One Piece", &firstEpisodeDate21SeasonOnePiece, 0, "", 0, true, false},
		{"One Piece", nil, 0, "https://anilist.co/anime/21", 0, false, false},
	}

	for _, tt := range tests {
		result, err := GetAnilistURLAndScore(tt.title, tt.firstEpisodeDate, tt.offset)

		if err != nil && !tt.expectError {
			t.Errorf("expected no error but got: %v", err)
		}

		if err == nil && tt.expectError {
			t.Errorf("expected an error but got none")
		}

		if result.AnilistURL != tt.expectedURL {
			t.Errorf("expected URL %v but got %v", tt.expectedURL, result.AnilistURL)
		}

		if tt.expectScore && result.AnilistScore != tt.expectedScore {
			t.Errorf("expected score %v but got %v", tt.expectedScore, result.AnilistScore)
		}
	}
}