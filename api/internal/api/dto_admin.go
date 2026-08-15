package api

import (
	"time"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
)

// Los mismos DTOs que en dto.go, para la parte de administración: bandeja,
// alias, fuentes y barridos. Mismo motivo: que el nombre de una columna no sea
// el nombre de un campo del JSON.
//
// La credencial de Google de una fuente NO sale nunca: es el contenido de la
// cuenta de servicio, y en el panel solo hace falta saber si la fuente tiene una.

// InboxFile es un fichero que llegó sin dueño y espera que alguien lo asigne.
type InboxFile struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	FolderPath  *string    `json:"folderPath"`
	Status      string     `json:"status"`
	Error       *string    `json:"error"`
	SellerID    *string    `json:"sellerId"`
	BranchID    *string    `json:"branchId"`
	Date        *string    `json:"date"`
	DateSource  string     `json:"dateSource"`
	Points      int32      `json:"points"`
	ValidPoints int32      `json:"validPoints"`
	FirstFix    *time.Time `json:"firstFix"`
	LastFix     *time.Time `json:"lastFix"`
	AliasHint   *string    `json:"aliasHint"`
	ImportedAt  time.Time  `json:"importedAt"`
	Source      string     `json:"source"`
}

func aInboxFile(f almacen.BandejaRow) InboxFile {
	var fecha *string
	if f.Fecha != nil {
		s := f.Fecha.Format(iso)
		fecha = &s
	}
	return InboxFile{
		ID: f.ID, Name: f.Nombre, FolderPath: f.RutaCarpeta,
		Status: string(f.Estado), Error: f.Error,
		SellerID: f.TrabajadorID, BranchID: f.SucursalID, Date: fecha,
		DateSource: string(f.OrigenFecha),
		Points:     f.PuntosTotal, ValidPoints: f.PuntosValidos,
		FirstFix: f.PrimerFix, LastFix: f.UltimoFix,
		AliasHint: f.PistaAlias, ImportedAt: f.ImportadoAt, Source: f.Fuente,
	}
}

// aAssignedFile: lo que devuelve asignar un fichero de la bandeja. Es la misma
// forma que InboxFile, pero viene de la tabla y no de la consulta de bandeja, así
// que no trae el nombre de la fuente.
func aAssignedFile(f almacen.GpxFile) InboxFile {
	var fecha *string
	if f.Fecha != nil {
		d := f.Fecha.Format(iso)
		fecha = &d
	}
	return InboxFile{
		ID: f.ID, Name: f.Nombre, FolderPath: f.RutaCarpeta,
		Status: string(f.Estado), Error: f.Error,
		SellerID: f.TrabajadorID, BranchID: f.SucursalID, Date: fecha,
		DateSource: string(f.OrigenFecha),
		Points:     f.PuntosTotal, ValidPoints: f.PuntosValidos,
		FirstFix: f.PrimerFix, LastFix: f.UltimoFix,
		AliasHint: f.PistaAlias, ImportedAt: f.ImportadoAt,
	}
}

// DeviceAlias recuerda que un nombre de dispositivo es de un vendedor concreto,
// para no volver a preguntarlo en la bandeja.
type DeviceAlias struct {
	ID            string    `json:"id"`
	Alias         string    `json:"alias"`
	OriginalAlias string    `json:"originalAlias"`
	SellerID      string    `json:"sellerId"`
	Seller        string    `json:"seller"`
	BranchID      *string   `json:"branchId"`
	CreatedAt     time.Time `json:"createdAt"`
}

func aDeviceAlias(a almacen.AliasDeSucursalRow) DeviceAlias {
	return DeviceAlias{
		ID: a.ID, Alias: a.Alias, OriginalAlias: a.AliasOriginal,
		SellerID: a.TrabajadorID, Seller: a.Trabajador,
		BranchID: a.SucursalID, CreatedAt: a.CreatedAt,
	}
}

// DriveSource es una carpeta de Drive de la que se leen los .gpx.
type DriveSource struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	FolderID      string     `json:"folderId"`
	Type          string     `json:"type"`
	BranchID      *string    `json:"branchId"`
	SellerID      *string    `json:"sellerId"`
	Active        bool       `json:"active"`
	HasCredential bool       `json:"hasCredential"`
	LastScan      *time.Time `json:"lastScan"`
	LastError     *string    `json:"lastError"`
	CreatedAt     time.Time  `json:"createdAt"`
}

func aDriveSource(f almacen.DriveSource) DriveSource {
	return DriveSource{
		ID: f.ID, Name: f.Nombre, FolderID: f.FolderID, Type: string(f.Tipo),
		BranchID: f.SucursalID, SellerID: f.TrabajadorID, Active: f.Activa,
		HasCredential: f.Credencial != "",
		LastScan:      f.UltimoBarrido, LastError: f.UltimoError, CreatedAt: f.CreatedAt,
	}
}

// ScanLog es lo que pasó en un barrido: cuántos ficheros se vieron y cuántos
// entraron.
type ScanLog struct {
	ID             string     `json:"id"`
	SourceID       *string    `json:"sourceId"`
	Type           string     `json:"type"`
	Start          time.Time  `json:"start"`
	End            *time.Time `json:"end"`
	FilesSeen      int32      `json:"filesSeen"`
	FilesNew       int32      `json:"filesNew"`
	FilesFailed    int32      `json:"filesFailed"`
	PointsInserted int32      `json:"pointsInserted"`
	Ok             bool       `json:"ok"`
	Detail         *string    `json:"detail"`
}

func aScanLog(l almacen.ImportLog) ScanLog {
	return ScanLog{
		ID: l.ID, SourceID: l.SourceID, Type: l.Tipo,
		Start: l.Inicio, End: l.Fin,
		FilesSeen: l.FicherosVistos, FilesNew: l.FicherosNuevos, FilesFailed: l.FicherosError,
		PointsInserted: l.PuntosInsertados, Ok: l.Ok, Detail: l.Detalle,
	}
}

func aInboxFiles(fs []almacen.BandejaRow) []InboxFile {
	out := make([]InboxFile, 0, len(fs))
	for _, f := range fs {
		out = append(out, aInboxFile(f))
	}
	return out
}

func aDeviceAliases(as []almacen.AliasDeSucursalRow) []DeviceAlias {
	out := make([]DeviceAlias, 0, len(as))
	for _, a := range as {
		out = append(out, aDeviceAlias(a))
	}
	return out
}

func aDriveSources(fs []almacen.DriveSource) []DriveSource {
	out := make([]DriveSource, 0, len(fs))
	for _, f := range fs {
		out = append(out, aDriveSource(f))
	}
	return out
}

func aScanLogs(ls []almacen.ImportLog) []ScanLog {
	out := make([]ScanLog, 0, len(ls))
	for _, l := range ls {
		out = append(out, aScanLog(l))
	}
	return out
}
