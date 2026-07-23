package db

import (
	"context"
	"errors"
	"log"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

func ErrorToHttpError(w http.ResponseWriter, err error) {
	// 1. Check for "Not Found"
	if errors.Is(err, mongo.ErrNoDocuments) {
		http.Error(w, "Resource not found", http.StatusNotFound)
		return
	}

	// 2. Check for Duplicate Key / Write Exception
	if writeExc, ok := errors.AsType[mongo.WriteException](err); ok {
		for _, we := range writeExc.WriteErrors {
			// Code 11000 is the specific MongoDB code for Duplicate Key
			if we.Code == 11000 {
				http.Error(w, "Already exists (duplicate key)", http.StatusConflict)
				return
			}
		}
		// If we are here, it was a WriteException but NOT a duplicate key
		http.Error(w, "Database write error", http.StatusInternalServerError)
		return
	}

	// 3. Check for Timeout
	if errors.Is(err, context.DeadlineExceeded) {
		http.Error(w, "Database timeout", http.StatusGatewayTimeout)
		return
	}

	// 4. Default / Unknown Error
	// Always log the real error for your own debugging
	log.Printf("Internal Server Error: %v", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}
