package fitbit

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

	"github.com/pkg/errors"
)

type Fitbit struct {
	ClientID     string `split_words:"true"`
	ClientSecret string `split_words:"true"`
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

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	UserID       string `json:"user_id"`
}

func (f *Fitbit) getToken(token *TokenResponse, code string) error {
	oauthURL := "https://api.fitbit.com/oauth2/token"
	data := url.Values{}
	if token.RefreshToken == "" {
		data.Set("clientId", f.ClientID)
		data.Set("grant_type", "authorization_code")
		data.Set("code", code)
	} else {
		data.Set("grant_type", "refresh_token")
		data.Set("refresh_token", token.RefreshToken)
	}

	idsecr := fmt.Sprintf("%s:%s", f.ClientID, f.ClientSecret)
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

func (f *Fitbit) loadTokens(refresh bool) (string, error) {
	file, err := os.Open(".token")
	if err != nil {
		if os.IsNotExist(err) {
			return f.server()
		}
		return "", errors.WithStack(err)
	}

	var tokenResponse TokenResponse
	err = json.NewDecoder(file).Decode(&tokenResponse)
	if err != nil {
		return "", errors.WithStack(err)
	}

	if refresh {
		err = f.getToken(&tokenResponse, "")
		if err != nil {
			return f.server()
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

func (f *Fitbit) server() (string, error) {
	mux := http.NewServeMux()
	srv := &http.Server{Addr: ":3000", Handler: mux}

	var tokenResponse TokenResponse
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rurl := "https://www.fitbit.com/oauth2/authorize?response_type=code&client_id=" + f.ClientID + "&scope=weight&expires_in=604800"
		http.Redirect(w, r, rurl, http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		code := query.Get("code")
		if code == "" {
			log.Println("need code query")
			return
		}

		err := f.getToken(&tokenResponse, code)
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

func (f *Fitbit) GetWeight(dt time.Time) (*FitbitWeight, error) {
	url := fmt.Sprintf("https://api.fitbit.com/1/user/-/body/log/weight/date/%s.json", dt.Format("2006-01-02"))

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	token, err := f.loadTokens(false)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	req.Header.Add("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if res.StatusCode != http.StatusOK {
		token, err := f.loadTokens(true)
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

func NewClient(id, secret string) *Fitbit {
	return &Fitbit{ClientID: id, ClientSecret: secret}
}
