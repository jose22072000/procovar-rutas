// Comando migrar: aplica y revierte el esquema de la base.
//
// Usa golang-migrate como biblioteca, no como binario suelto: así el despliegue
// lleva un solo ejecutable y no hay que acordarse de instalar una herramienta
// aparte en el servidor. Las migraciones van EMPOTRADAS en el binario con
// go:embed, de modo que no puede desplegarse un binario sin sus migraciones ni
// aplicarse unas migraciones que no correspondan a ese binario.
//
//	migrar up          aplica todo lo pendiente
//	migrar down 1      revierte la última
//	migrar version     dice en qué versión está la base
//	migrar force 3     desatasca una migración marcada como sucia
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/procovar/procovar-rutas/api/migraciones"
)

func main() {
	log.SetFlags(0)

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("falta DATABASE_URL")
	}

	orden := "up"
	if len(os.Args) > 1 {
		orden = os.Args[1]
	}

	fuente, err := iofs.New(migraciones.FS, ".")
	if err != nil {
		log.Fatalf("no se pudieron leer las migraciones empotradas: %v", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", fuente, url)
	if err != nil {
		log.Fatalf("no se pudo conectar con la base: %v", err)
	}
	defer m.Close()

	switch orden {
	case "up":
		err = m.Up()
	case "down":
		pasos := 1
		if len(os.Args) > 2 {
			pasos, err = strconv.Atoi(os.Args[2])
			if err != nil {
				log.Fatalf("número de pasos inválido: %v", err)
			}
		}
		// Siempre por pasos, nunca m.Down() entero: un `down` completo en el
		// servidor equivocado borra la base sin preguntar.
		err = m.Steps(-pasos)
	case "version":
		v, sucia, verr := m.Version()
		if errors.Is(verr, migrate.ErrNilVersion) {
			fmt.Println("base sin migrar")
			return
		}
		if verr != nil {
			log.Fatalf("no se pudo leer la versión: %v", verr)
		}
		estado := "limpia"
		if sucia {
			estado = "SUCIA — hay que revisar a mano y luego `migrar force <v>`"
		}
		fmt.Printf("versión %d (%s)\n", v, estado)
		return
	case "force":
		if len(os.Args) < 3 {
			log.Fatal("uso: migrar force <version>")
		}
		v, cerr := strconv.Atoi(os.Args[2])
		if cerr != nil {
			log.Fatalf("versión inválida: %v", cerr)
		}
		err = m.Force(v)
	default:
		log.Fatalf("orden desconocida: %q (up | down [n] | version | force <v>)", orden)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("sin cambios: la base ya estaba al día")
		return
	}
	if err != nil {
		log.Fatalf("la migración falló: %v", err)
	}

	fmt.Println("migración aplicada")
}
