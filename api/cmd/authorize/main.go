// Command authorize: obtains the refresh token for a Google account.
//
// There is one Google account per branch, and reading its folders needs one
// refresh token per account. n8n already has them, but stores them encrypted and
// does not show them, so this command performs the same OAuth dance and prints
// them in the format GOOGLE_CUENTAS expects.
//
//	go run ./cmd/authorize -clave granma \
//	    -client-id  319113730289-….apps.googleusercontent.com \
//	    -client-secret GOCSPX-…
//
// Open the URL it prints, sign in WITH THAT BRANCH'S ACCOUNT, and paste the code
// back. The scope it asks for is READ-ONLY: this system never moves or deletes
// anything in the sellers' Drive.
//
// Note: the client id and secret are the Google Cloud application's (the same ones
// n8n uses). For the flow to complete, that application has to allow
// `http://localhost` as a redirect URL — otherwise Google answers
// redirect_uri_mismatch.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

func main() {
	clave := flag.String("clave", "", "nombre de la cuenta (p. ej. granma); es lo que apuntan las carpetas")
	clientID := flag.String("client-id", "", "identificador de cliente de Google")
	clientSecret := flag.String("client-secret", "", "secreto de cliente de Google")
	redirect := flag.String("redirect", "http://localhost", "URL de redirección registrada en Google")
	flag.Parse()

	if *clave == "" || *clientID == "" || *clientSecret == "" {
		fmt.Fprintln(os.Stderr, "faltan -clave, -client-id o -client-secret")
		flag.Usage()
		os.Exit(1)
	}

	cfg := &oauth2.Config{
		ClientID:     *clientID,
		ClientSecret: *clientSecret,
		RedirectURL:  *redirect,
		Endpoint:     google.Endpoint,
		Scopes:       []string{drive.DriveReadonlyScope},
	}

	// AccessTypeOffline + prompt=consent is what makes Google hand over the refresh
	// token. Without forcing consent, the second authorization of the same account
	// returns only the access token, which expires in an hour and leaves ingest dead
	// the next day.
	url := cfg.AuthCodeURL("procovar-rutas",
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"))

	fmt.Println("1. Abre esta dirección en el navegador:")
	fmt.Println()
	fmt.Println("  ", url)
	fmt.Println()
	fmt.Printf("2. Entra con la cuenta de Google de la sucursal %q y acepta.\n", *clave)
	fmt.Println("3. Te llevará a una página que no carga: copia el valor de `code=` de la barra")
	fmt.Println("   de direcciones y pégalo aquí.")
	fmt.Println()
	fmt.Print("Código: ")

	lector := bufio.NewReader(os.Stdin)
	codigo, err := lector.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "no se pudo leer el código: %v\n", err)
		os.Exit(1)
	}
	codigo = strings.TrimSpace(codigo)

	token, err := cfg.Exchange(context.Background(), codigo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Google rechazó el código: %v\n", err)
		os.Exit(1)
	}
	if token.RefreshToken == "" {
		fmt.Fprintln(os.Stderr,
			"Google no devolvió token de refresco. Suele pasar cuando la cuenta ya había\n"+
				"autorizado esta aplicación: quita el acceso en myaccount.google.com/permissions\n"+
				"y vuelve a intentarlo.")
		os.Exit(1)
	}

	cuenta := map[string]string{
		"clave":        *clave,
		"tipo":         "oauth",
		"clientId":     *clientID,
		"clientSecret": *clientSecret,
		"refreshToken": token.RefreshToken,
	}
	salida, _ := json.MarshalIndent(cuenta, "", "  ")

	fmt.Println()
	fmt.Println("Listo. Añade este objeto a la lista de GOOGLE_CUENTAS:")
	fmt.Println()
	fmt.Println(string(salida))
}
