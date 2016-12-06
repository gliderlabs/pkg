package auth0

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"golang.org/x/oauth2"
)

const (
	usersEndpoint      = "https://%s/api/v2/users/%s"
	authorizeEndpoint  = "https://%s/authorize"
	userInfoEndpoint   = "https://%s/userinfo"
	tokenEndpoint      = "https://%s/oauth/token"
	delegationEndpoint = "https://%s/delegation"
)

type Client struct {
	ClientID     string
	ClientSecret string
	Domain       string
	CallbackURL  string
	Token        string
	Scopes       []string
}

type UserInfo map[string]interface{}

type User map[string]interface{}

func (c *Client) LogoutURL(returnTo string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	if returnTo != "" {
		q.Set("returnTo", returnTo)
	}
	logoutURL := &url.URL{
		Scheme:   "https",
		Host:     c.Domain,
		Path:     "/v2/logout",
		RawQuery: q.Encode(),
	}
	return logoutURL.String()
}

func (c *Client) oauthConfig() *oauth2.Config {
	if c.Scopes == nil {
		c.Scopes = []string{"openid", "profile"}
	}
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.CallbackURL,
		Scopes:       c.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  fmt.Sprintf(authorizeEndpoint, c.Domain),
			TokenURL: fmt.Sprintf(tokenEndpoint, c.Domain),
		},
	}
}

func (c *Client) NewToken(code string) (*oauth2.Token, error) {
	return c.oauthConfig().Exchange(oauth2.NoContext, code)
}

func (c *Client) get(token *oauth2.Token, url string) (map[string]interface{}, error) {
	client := c.oauthConfig().Client(oauth2.NoContext, token)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var obj map[string]interface{}
	err = json.Unmarshal(data, &obj)
	return obj, err
}

func (c *Client) UserInfo(token *oauth2.Token) (UserInfo, error) {
	resp, err := c.get(token, fmt.Sprintf(userInfoEndpoint, c.Domain))
	return UserInfo(resp), err
}

func (c *Client) DelegationToken(token *oauth2.Token, apiType string) (string, error) {
	body, err := json.Marshal(map[string]interface{}{
		"client_id":  c.ClientID,
		"grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
		"id_token":   token.Extra("id_token"),
		"target":     c.ClientID,
		"scope":      "openid name email",
		"api_type":   apiType,
	})
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf(delegationEndpoint, c.Domain)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := new(http.Client).Do(req)
	if err != nil {
		return "", err
	}
	raw, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return "", err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", err
	}
	return obj["id_token"].(string), nil
}

func (c *Client) User(id string) (User, error) {
	url := fmt.Sprintf(usersEndpoint, c.Domain, id)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	resp, err := new(http.Client).Do(req)
	if err != nil {
		return nil, err
	}
	raw, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var user User
	if err := json.Unmarshal(raw, &user); err != nil {
		return nil, err
	}
	return user, nil
}

func (c *Client) PatchUser(id string, user User) error {
	body, err := json.Marshal(user)
	if err != nil {
		return err
	}
	url := fmt.Sprintf(usersEndpoint, c.Domain, id)
	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := new(http.Client).Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		raw, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		if err != nil {
			return err
		}
		var errBody map[string]interface{}
		if err := json.Unmarshal(raw, &errBody); err != nil {
			return err
		}
		return fmt.Errorf("%s: %s", errBody["error"], errBody["message"])
	}
	return nil
}
