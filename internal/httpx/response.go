package httpx

import (
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, v any) {

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func Error(w http.ResponseWriter, err error) {

	w.WriteHeader(http.StatusInternalServerError)

	json.NewEncoder(w).Encode(map[string]string{
		"error": err.Error(),
	})
}
