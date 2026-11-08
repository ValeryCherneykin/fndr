package preview

import "bytes"

// fileType pairs a file signature with a name a person understands.
type fileType struct {
	magic string
	name  string
}

// fileTypes are the common formats a preview card can name by their
// signature, the same way opener tells programs apart.
var fileTypes = []fileType{
	{"%PDF", "PDF document"},
	{"\x89PNG", "PNG image"},
	{"\xff\xd8\xff", "JPEG image"},
	{"GIF8", "GIF image"},
	{"PK\x03\x04", "ZIP archive"}, // also .docx, .xlsx, .jar: all zip inside
	{"\x1f\x8b", "gzip archive"},
	{"SQLite format 3\x00", "SQLite database"},
}

// typeName names the format of a file from its first bytes, or "binary
// file" when the signature is not one of fileTypes.
func typeName(head []byte) string {
	for _, t := range fileTypes {
		if bytes.HasPrefix(head, []byte(t.magic)) {
			return t.name
		}
	}
	return "binary file"
}
