package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	goskyutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

type Config struct {
	ClientID     string `split_words:"true"`
	ClientSecret string `split_words:"true"`
	BskyHost     string `split_words:"true" default:"https://bsky.social"`
	BskyHandle   string `split_words:"true"`
	BskyPassword string `split_words:"true"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	UserID       string `json:"user_id"`
}

type FitbitWeight struct {
	Weights []struct {
		BMI    float64 `json:"bmi"`
		Date   string  `json:"date"`
		Fat    float64 `json:"fat"`
		LogID  uint    `json:"logId"`
		Source string  `json:"source"`
		Time   string  `json:"time"`
		Weight float64 `json:"weight"`
	} `json:"weight"`
}

func main() {
	var c Config
	err := envconfig.Process("f2b", &c)
	if err != nil {
		log.Fatal(err)
	}

	app := &cli.App{
		Name:  "fit2bsky",
		Usage: "Post your weight data recorded on fitbit to bluesky",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "date",
				Usage:       "Date weight was recorded (e.g. 2006-01-02)",
				Aliases:     []string{"d"},
				Value:       "",
				DefaultText: "today",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Only weight data acquisition is performed.",
			},
		},
		Action: func(ctx *cli.Context) error {
			dateStr := ctx.String("date")
			date := time.Now()
			if dateStr != "" {
				date, err = time.Parse("2006-01-02", dateStr)
				if err != nil {
					return errors.WithStack(err)
				}
			}
			result, err := getWeight(&c, date)
			if err != nil {
				return err
			}

			w := result.Weights[0]
			text := fmt.Sprintf("今日の体重: %4.1fkg (BMI: %4.2f ) 体脂肪率: %4.2f%% via Fitbit\n", w.Weight, w.BMI, w.Fat)

			if ctx.Bool("dry-run") {
				fmt.Println(text)
				return nil
			}

			return postBluesky(&c, text)
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("%+v", err)
	}
}

func postBluesky(c *Config, text string) error {
	xrpcc := &xrpc.Client{
		Client: goskyutil.NewHttpClient(),
		Host:   c.BskyHost,
		Auth:   &xrpc.AuthInfo{Handle: c.BskyHandle},
	}
	auth, err := atproto.ServerCreateSession(context.TODO(), xrpcc, &atproto.ServerCreateSession_Input{
		Identifier: xrpcc.Auth.Handle,
		Password:   c.BskyPassword,
	})
	if err != nil {
		return errors.WithStack(err)
	}
	xrpcc.Auth.Did = auth.Did
	xrpcc.Auth.AccessJwt = auth.AccessJwt
	xrpcc.Auth.RefreshJwt = auth.RefreshJwt

	post := &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          text,
		CreatedAt:     time.Now().Local().Format(time.RFC3339),
	}

	resp, err := atproto.RepoCreateRecord(context.TODO(), xrpcc, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       xrpcc.Auth.Did,
		Record: &lexutil.LexiconTypeDecoder{
			Val: post,
		},
	})
	if err != nil {
		return errors.WithStack(err)
	}

	log.Printf("posted: %s\n", resp.Uri)

	return nil
}

func getToken(c *Config, token *TokenResponse, code string) error {
	oauthURL := "https://api.fitbit.com/oauth2/token"
	data := url.Values{}
	if token.RefreshToken == "" {
		data.Set("clientId", c.ClientID)
		data.Set("grant_type", "authorization_code")
		data.Set("code", code)
	} else {
		data.Set("grant_type", "refresh_token")
		data.Set("refresh_token", token.RefreshToken)
	}

	idsecr := fmt.Sprintf("%s:%s", c.ClientID, c.ClientSecret)
	encids := base64.StdEncoding.EncodeToString([]byte(idsecr))

	req, err := http.NewRequest("POST", oauthURL, strings.NewReader(data.Encode()))
	if err != nil {
		return errors.WithStack(err)
	}

	req.Header.Add("Authorization", "Basic "+encids)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return errors.WithStack(err)
		}
		log.Println(res.Status + ": " + string(body))
	}

	err = json.NewDecoder(res.Body).Decode(token)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func loadTokens(c *Config, refresh bool) (string, error) {
	file, err := os.Open(".token")
	if err != nil {
		if os.IsNotExist(err) {
			return server(c)
		}
		return "", errors.WithStack(err)
	}

	var tokenResponse TokenResponse
	err = json.NewDecoder(file).Decode(&tokenResponse)
	if err != nil {
		return "", errors.WithStack(err)
	}

	if refresh {
		err = getToken(c, &tokenResponse, "")
		if err != nil {
			return "", errors.WithStack(err)
		}
		file, err := os.Create(".token")
		if err != nil {
			return "", errors.WithStack(err)
		}
		err = json.NewEncoder(file).Encode(tokenResponse)
		if err != nil {
			return "", errors.WithStack(err)
		}
	}

	return tokenResponse.AccessToken, nil
}

func getWeight(c *Config, dt time.Time) (*FitbitWeight, error) {
	url := fmt.Sprintf("https://api.fitbit.com/1/user/-/body/log/weight/date/%s.json", dt.Format("2006-01-02"))

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	token, err := loadTokens(c, false)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	req.Header.Add("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if res.StatusCode != http.StatusOK {
		token, err := loadTokens(c, true)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		res, err = client.Do(req)
		if err != nil {
			return nil, errors.WithStack(err)
		}
	}
	defer res.Body.Close()

	var w FitbitWeight
	err = json.NewDecoder(res.Body).Decode(&w)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &w, nil
}

func server(c *Config) (string, error) {
	mux := http.NewServeMux()
	srv := &http.Server{Addr: ":3000", Handler: mux}

	var tokenResponse TokenResponse
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rurl := "https://www.fitbit.com/oauth2/authorize?response_type=code&client_id=" + c.ClientID + "&scope=weight&expires_in=604800"
		http.Redirect(w, r, rurl, http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		code := query.Get("code")
		if code == "" {
			log.Println("need code query")
			return
		}

		err := getToken(c, &tokenResponse, code)
		if err != nil {
			log.Printf("%+v", err)
			return
		}

		file, err := os.Create(".token")
		if err != nil {
			log.Printf("%+v", err)
			return
		}
		err = json.NewEncoder(file).Encode(tokenResponse)
		if err != nil {
			log.Printf("%+v", err)
			return
		}

		err = srv.Shutdown(context.TODO())
		if err != nil {
			log.Printf("%+v", err)
			return
		}
	})

	fmt.Println("Please access to http://localhost:3000/")

	err := srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return "", errors.WithStack(err)
	}

	return tokenResponse.AccessToken, nil
}
