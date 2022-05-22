package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	spotifyauth "github.com/zmb3/spotify/v2/auth"

	"github.com/zmb3/spotify/v2"
)

// redirectURI is the OAuth redirect URI for the application.
// You must register an application at Spotify's developer portal
// and enter this value.
const redirectURI = "http://localhost:8080/callback"

var (
	auth = spotifyauth.New(
		spotifyauth.WithRedirectURL(redirectURI),
		spotifyauth.WithScopes(
			spotifyauth.ScopeUserReadPrivate,
			spotifyauth.ScopeUserFollowRead,
			spotifyauth.ScopePlaylistModifyPublic,
			spotifyauth.ScopePlaylistModifyPrivate,
		),
	)
	ch    = make(chan *spotify.Client)
	state = "abc123"
)

func main() {
	// first start an HTTP server
	http.HandleFunc("/callback", completeAuth)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Got request for:", r.URL.String())
	})
	go func() {
		err := http.ListenAndServe(":8080", nil)
		if err != nil {
			log.Fatal(err)
		}
	}()

	url := auth.AuthURL(state)
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// wait for auth to complete
	client := <-ch

	user, err := client.CurrentUser(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	playlistName := time.Now().Format("2006-01-02")
	playlist := createPlaylist(client, user.ID, playlistName)
	populatePlaylist(client, playlist.ID)
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(r.Context(), state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}
	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}

	// use the token to get an authenticated client
	client := spotify.New(auth.Client(r.Context(), tok), spotify.WithRetry(true))
	fmt.Fprintf(w, "Login Completed!")
	ch <- client
}

func createPlaylist(client *spotify.Client, userId, playlistName string) spotify.FullPlaylist {
	result, err := client.CreatePlaylistForUser(context.Background(), userId, playlistName, "Play all", false, false)
	if err != nil {
		log.Fatal(err)
	}
	return *result
}

const itemsLimit = 50

func populatePlaylist(client *spotify.Client, playlistId spotify.ID) {
	options := []spotify.RequestOption{spotify.Limit(itemsLimit)}
	counter := 0
	for {
		artists, err := client.CurrentUsersFollowedArtists(context.Background(), options...)
		if err != nil {
			log.Fatal(err)
		}
		if len(artists.Artists) == 0 {
			break
		}
		for _, artist := range artists.Artists {
			counter++
			fmt.Printf("artist #%03d ID: %v, Name: %v\n", counter, artist.ID, artist.Name)
			populatePlaylistWithArtistAlbums(client, playlistId, artist.ID)
		}
		last := artists.Artists[len(artists.Artists)-1].ID.String()
		options = []spotify.RequestOption{options[0], spotify.After(last)}
	}
}

func populatePlaylistWithArtistAlbums(client *spotify.Client, playlistId, artistId spotify.ID) {
	options := []spotify.RequestOption{spotify.Offset(0), spotify.Limit(itemsLimit)}
	counter := 0
	var albumPresent = make(map[string]bool)
	for {
		albums, err := client.GetArtistAlbums(
			context.Background(),
			artistId,
			[]spotify.AlbumType{spotify.AlbumTypeAlbum, spotify.AlbumTypeSingle},
			options...,
		)
		if err != nil {
			log.Fatal(err)
		}
		if len(albums.Albums) == 0 {
			break
		}
		for _, album := range albums.Albums {
			counter++
			releaseYearLength := 4
			if releaseYearLength > len(album.ReleaseDate) {
				releaseYearLength = len(album.ReleaseDate)
			}
			releaseDate := album.ReleaseDate[:releaseYearLength]
			key := strings.ToLower(album.Name + releaseDate)
			if !albumPresent[key] {
				albumPresent[key] = true
				fmt.Printf("    %s #%03d ID: %v, Name: %v %v\n", album.AlbumType, counter, album.ID, album.Name, releaseDate)
				populatePlaylistWithAlbumTracks(client, playlistId, album.ID)
			}
		}
		options[0] = spotify.Offset(counter)
	}
}

func populatePlaylistWithAlbumTracks(client *spotify.Client, playlistId, albumId spotify.ID) {
	options := []spotify.RequestOption{spotify.Offset(0), spotify.Limit(itemsLimit)}
	counter := 0
	var albumTracks []spotify.ID
	for {
		tracks, err := client.GetAlbumTracks(context.Background(), albumId, options...)
		if err != nil {
			log.Fatal(err)
		}
		if len(tracks.Tracks) == 0 {
			break
		}
		for _, track := range tracks.Tracks {
			counter++
			albumTracks = append(albumTracks, track.ID)
			fmt.Printf("        track #%03d ID: %v, Name: %v\n", counter, track.ID, track.Name)
		}
		options[0] = spotify.Offset(counter)
	}
	for index := 0; index < len(albumTracks); {
		high := index + itemsLimit
		if high > len(albumTracks) {
			high = len(albumTracks)
		}
		client.AddTracksToPlaylist(context.Background(), playlistId, albumTracks[index:high]...)
		index = high
	}
}
