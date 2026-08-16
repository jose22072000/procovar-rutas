package api

import (
	"time"

	"github.com/procovar/procovar-rutas/api/internal/store"
)

// The same DTOs as in dto.go, for the administration side: inbox, aliases,
// sources and scans. Same reason: a column's name should not be a JSON field's
// name.
//
// A source's Google credential NEVER goes out: it is the service account's
// contents, and the panel only needs to know whether the source has one.

// InboxFile is a file that arrived without an owner and waits to be assigned.
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

func aInboxFile(f store.InboxRow) InboxFile {
	var fecha *string
	if f.Date != nil {
		s := f.Date.Format(iso)
		fecha = &s
	}
	return InboxFile{
		ID: f.ID, Name: f.Name, FolderPath: f.FolderPath,
		Status: string(f.Status), Error: f.Error,
		SellerID: f.SellerID, BranchID: f.BranchID, Date: fecha,
		DateSource: string(f.DateSource),
		Points:     f.TotalPoints, ValidPoints: f.ValidPoints,
		FirstFix: f.FirstFix, LastFix: f.LastFix,
		AliasHint: f.AliasHint, ImportedAt: f.ImportedAt, Source: f.Source,
	}
}

// aAssignedFile: what assigning a file from the inbox returns. Same shape as
// InboxFile, but it comes from the table rather than the inbox query, so it does
// not carry the source name.
func aAssignedFile(f store.GpxFile) InboxFile {
	var fecha *string
	if f.Date != nil {
		d := f.Date.Format(iso)
		fecha = &d
	}
	return InboxFile{
		ID: f.ID, Name: f.Name, FolderPath: f.FolderPath,
		Status: string(f.Status), Error: f.Error,
		SellerID: f.SellerID, BranchID: f.BranchID, Date: fecha,
		DateSource: string(f.DateSource),
		Points:     f.TotalPoints, ValidPoints: f.ValidPoints,
		FirstFix: f.FirstFix, LastFix: f.LastFix,
		AliasHint: f.AliasHint, ImportedAt: f.ImportedAt,
	}
}

// DeviceAlias remembers that a device name belongs to a particular seller, so the
// inbox never asks again.
type DeviceAlias struct {
	ID            string    `json:"id"`
	Alias         string    `json:"alias"`
	OriginalAlias string    `json:"originalAlias"`
	SellerID      string    `json:"sellerId"`
	Seller        string    `json:"seller"`
	BranchID      *string   `json:"branchId"`
	CreatedAt     time.Time `json:"createdAt"`
}

func aDeviceAlias(a store.BranchAliasesRow) DeviceAlias {
	return DeviceAlias{
		ID: a.ID, Alias: a.Alias, OriginalAlias: a.OriginalAlias,
		SellerID: a.SellerID, Seller: a.Seller,
		BranchID: a.BranchID, CreatedAt: a.CreatedAt,
	}
}

// DriveSource is a Drive folder the .gpx files are read from.
type DriveSource struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Branch        string     `json:"branch"`
	FolderID      string     `json:"folderId"`
	Type          string     `json:"type"`
	BranchID      *string    `json:"branchId"`
	SellerID      *string    `json:"sellerId"`
	Active        bool       `json:"active"`
	HasCredential bool       `json:"hasCredential"`
	LastScan      *time.Time `json:"lastScan"`
	LastError     *string    `json:"lastError"`
	CreatedAt     time.Time  `json:"createdAt"`
	// Files, LastFile y DaysSilent contestan la pregunta que se le hace a esta
	// pantalla: a qué vendedor le falla el GPS y desde cuándo. LastFile va vacía y
	// DaysSilent en -1 cuando por esa carpeta no ha entrado nunca una ruta.
	Files      int64  `json:"files"`
	LastFile   string `json:"lastFile"`
	DaysSilent int32  `json:"daysSilent"`
}

func aDriveSourceConSucursal(f store.ActiveSourcesWithBranchRow) DriveSource {
	d := aDriveSource(store.DriveSource{
		ID: f.ID, Name: f.Name, FolderID: f.FolderID, Type: f.Type,
		BranchID: f.BranchID, SellerID: f.SellerID, Active: f.Active,
		Credential: f.Credential, LastScan: f.LastScan, LastError: f.LastError,
		CreatedAt: f.CreatedAt,
	})
	d.Branch = f.Branch
	d.Files = f.Ficheros
	d.LastFile = f.Ultima
	d.DaysSilent = f.DiasCallado
	return d
}

func aDriveSourcesConSucursal(fs []store.ActiveSourcesWithBranchRow) []DriveSource {
	out := make([]DriveSource, 0, len(fs))
	for _, f := range fs {
		out = append(out, aDriveSourceConSucursal(f))
	}
	return out
}

func aDriveSource(f store.DriveSource) DriveSource {
	return DriveSource{
		ID: f.ID, Name: f.Name, FolderID: f.FolderID, Type: string(f.Type),
		BranchID: f.BranchID, SellerID: f.SellerID, Active: f.Active,
		HasCredential: f.Credential != "",
		LastScan:      f.LastScan, LastError: f.LastError, CreatedAt: f.CreatedAt,
	}
}

// ScanLog is what happened during a scan: how many files were seen and how many
// went in.
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

func aScanLog(l store.ImportLog) ScanLog {
	return ScanLog{
		ID: l.ID, SourceID: l.SourceID, Type: l.Type,
		Start: l.Start, End: l.End,
		FilesSeen: l.FilesSeen, FilesNew: l.FilesNew, FilesFailed: l.FilesFailed,
		PointsInserted: l.InsertedPoints, Ok: l.Ok, Detail: l.Detail,
	}
}

func aInboxFiles(fs []store.InboxRow) []InboxFile {
	out := make([]InboxFile, 0, len(fs))
	for _, f := range fs {
		out = append(out, aInboxFile(f))
	}
	return out
}

func aDeviceAliases(as []store.BranchAliasesRow) []DeviceAlias {
	out := make([]DeviceAlias, 0, len(as))
	for _, a := range as {
		out = append(out, aDeviceAlias(a))
	}
	return out
}

func aDriveSources(fs []store.DriveSource) []DriveSource {
	out := make([]DriveSource, 0, len(fs))
	for _, f := range fs {
		out = append(out, aDriveSource(f))
	}
	return out
}

func aScanLogs(ls []store.ImportLog) []ScanLog {
	out := make([]ScanLog, 0, len(ls))
	for _, l := range ls {
		out = append(out, aScanLog(l))
	}
	return out
}
