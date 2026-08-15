// Comando autorizar: consigue el token de refresco de una cuenta de Google.
//
// Hay una cuenta de Google por sucursal, y para leer sus carpetas hace falta un
// token de refresco por cuenta. n8n ya los tiene, pero los guarda cifrados y no
// los enseña, así que este comando hace el mismo baile de OAuth y los imprime en
// el formato que espera GOOGLE_CUENTAS.
//
//	go run ./cmd/autorizar -clave granma \
//	    -client-id  319113730289-….apps.googleusercontent.com \
//	    -client-secret GOCSPX-…
//
// Abre la URL que imprime, entra CON LA CUENTA DE ESA SUCURSAL, y pega de vuelta
// el código. El permiso que pide es de SOLO LECTURA: este sistema nunca mueve ni
// borra nada del Drive de los trabajadores.
//
// Aviso: el identificador y el secreto son los de la aplicación de Google Cloud
// (los mismos que usa n8n). Para que el flujo termine, esa aplicación tiene que
// admitir `http://localhost` como URL de redirección — si no, Google contesta
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

	// AccessTypeOffline + prompt=consent es lo que hace que Google entregue el
	// token de refresco. Sin el consentimiento forzado, en la segunda
	// autorización de la misma cuenta devuelve solo el de acceso, que caduca en
	// una hora y deja la ingesta muerta al día siguiente.
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
