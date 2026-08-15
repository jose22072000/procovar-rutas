// Package migraciones empotra los ficheros .sql del esquema en el binario.
//
// go:embed solo alcanza el directorio del propio paquete, así que las
// migraciones se exponen desde aquí y el comando `migrar` las consume. La
// ventaja de empotrarlas no es la comodidad: es que resulta imposible desplegar
// un binario sin sus migraciones, o aplicar en un servidor unas migraciones que
// no correspondan a la versión del código que está corriendo.
package migraciones

import "embed"

//go:embed *.sql
var FS embed.FS
