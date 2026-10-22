// Command token generates a session token offline, including for older images.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/wppconnect-team/wppconnect-server-go/internal/auth"
)

func main() {
	name := flag.String("session", "", "WhatsApp session name")
	raw := flag.Bool("raw", false, "emit raw bcrypt for older Go images; use it in the Authorization header")
	flag.Parse()
	secret := os.Getenv("SECRET_KEY")
	if *name == "" || strings.Contains(*name, ":") || secret == "" {
		log.Fatal("set SECRET_KEY and pass -session with a nonempty name without ':'")
	}
	token, err := auth.Generate(*name, secret)
	if err != nil {
		log.Fatal(err)
	}
	if *raw {
		token = strings.NewReplacer("_", "/", "-", "+").Replace(token)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{
		"status": "success", "session": *name, "token": token, "full": *name + ":" + token,
	}); err != nil {
		log.Fatal(err)
	}
}
