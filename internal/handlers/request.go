package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

var (
	errInvalidRequestBody   = errors.New("invalid request body")
	errRequestBodyTooLarge  = errors.New("request body too large")
	errUnsupportedMediaType = errors.New("Content-Type must be application/json")
)

func decodeJSONRequest(w http.ResponseWriter, r *http.Request, maxBytes int64, request any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(request); err != nil {
		return requestBodyError(err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return requestBodyError(err)
	}

	return nil
}

func requestBodyError(err error) error {
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		return errRequestBodyTooLarge
	}
	return errInvalidRequestBody
}
