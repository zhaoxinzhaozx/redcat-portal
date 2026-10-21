package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"portal/internal/httpapi"
	"portal/internal/store"
	"portal/internal/sub2api"
)

func main() {
	ctx := context.Background()
	data, err := store.Open(ctx, databaseURL(), env("DB_SCHEMA", "portal"))
	if err != nil {
		log.Fatal(err)
	}
	defer data.Close()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			if err := data.DrawDue(context.Background(), time.Now()); err != nil {
				log.Printf("draw: %v", err)
			}
			<-ticker.C
		}
	}()

	api := sub2api.New(
		env("SUB2API_BASE_URL", "http://sub2api:8080"),
		os.Getenv("PORTAL_ADMIN_EMAIL"),
		os.Getenv("PORTAL_ADMIN_PASSWORD"),
		claimPrefix(),
	)
	server := httpapi.New(env("BASE_PATH", "/portal"), data, api, httpapi.Site{
		Name:      env("SITE_NAME", "站点"),
		Slogan:    os.Getenv("SITE_SLOGAN"),
		Contact:   os.Getenv("SUPPORT_CONTACT"),
		URL:       strings.TrimRight(os.Getenv("SITE_URL"), "/"),
		Downloads: httpapi.ParseDownloads(os.Getenv("DOWNLOADS")),
	})
	addr := ":" + env("PORT", "8081")
	log.Printf("portal listening on %s%s", addr, server.Base)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}

func databaseURL() string {
	if value := os.Getenv("DATABASE_URL"); value != "" {
		return value
	}
	user := url.UserPassword(env("POSTGRES_USER", "sub2api"), os.Getenv("POSTGRES_PASSWORD"))
	host := env("POSTGRES_HOST", "postgres")
	port := env("POSTGRES_PORT", "5432")
	name := env("POSTGRES_DB", "sub2api")
	return "postgres://" + user.String() + "@" + host + ":" + port + "/" + name + "?sslmode=disable"
}

func claimPrefix() string {
	value := env("CLAIM_KEY_PREFIX", "portal-claim")
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '-' || char == '_' {
			continue
		}
		log.Fatal("CLAIM_KEY_PREFIX contains unsupported characters")
	}
	return value
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
