package api0

import (
	"net/http"
)

type MainMenuPromos struct {
	NewInfo      MainMenuPromosNew         `json:"newInfo"`
	LargeButton  MainMenuPromosButtonLarge `json:"largeButton"`
	SmallButton1 MainMenuPromosButtonSmall `json:"smallButton1"`
	SmallButton2 MainMenuPromosButtonSmall `json:"smallButton2"`
}

type MainMenuPromosNew struct {
	Title1 string `json:"Title1"`
	Title2 string `json:"Title2"`
	Title3 string `json:"Title3"`
}

type MainMenuPromosButtonLarge struct {
	Title      string `json:"Title"`
	Text       string `json:"Text"`
	Url        string `json:"Url"`
	ImageIndex int    `json:"ImageIndex"`
}

type MainMenuPromosButtonSmall struct {
	Title      string `json:"Title"`
	Url        string `json:"Url"`
	ImageIndex int    `json:"ImageIndex"`
}

func (h *Handler) handleMainMenuPromos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions && r.Method != http.MethodHead && r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.Method == http.MethodOptions {
		w.Header().Set("Allow", "OPTIONS, HEAD, GET")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var p MainMenuPromos
	if h.MainMenuPromos != nil {
		p = h.MainMenuPromos(r)
	}
	respJSON(w, r, http.StatusOK, p)
}

/*
  /client/origin_auth:
    GET:
  /client/auth_with_server:
    POST:
  /client/auth_with_self:
    POST:

  /client/servers:
    GET:
*/
