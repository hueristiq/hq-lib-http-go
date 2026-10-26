package mime

// MIME represents an Internet Media Type (or content type).
//
// MIME is a defined string type, so it integrates with string-based APIs.
// Conversion from any string is allowed; the type does not validate values. The
// constants below cover common registered and de facto media types.
type MIME string

// String returns the underlying string representation of the MIME type.
//
// This method converts the MIME value (a string alias) into a plain string.
// It is useful when setting HTTP header values, logging, or debugging, where a standard
// string is required.
//
// Returns:
//   - mime (string): the MIME type as a plain string.
func (m MIME) String() (mime string) {
	mime = string(m)

	return mime
}

// Predefined MIME type constants.
//
// These constants cover commonly used media types, including IANA-registered
// types and de facto x- types. They are ordered by name for readability.
const (
	AACAudio              MIME = "audio/aac"
	AbiWordDocument       MIME = "application/x-abiword"
	AmazonKindleEBook     MIME = "application/vnd.amazon.ebook"
	AppleInstallerPackage MIME = "application/vnd.apple.installer+xml"
	ArchiveDocument       MIME = "application/x-freearc"
	AVIFImage             MIME = "image/avif"
	AVIVideo              MIME = "video/x-msvideo"
	BinaryData            MIME = "application/octet-stream"
	BitmapImage           MIME = "image/bmp"
	BourneShellScript     MIME = "application/x-sh"
	BZip2Archive          MIME = "application/x-bzip2"
	BZipArchive           MIME = "application/x-bzip"
	CDAudio               MIME = "application/x-cdf"
	CShellScript          MIME = "application/x-csh"
	CSS                   MIME = "text/css"
	CSV                   MIME = "text/csv"
	EPUB                  MIME = "application/epub+zip"
	EventStream           MIME = "text/event-stream"
	FormURLEncoded        MIME = "application/x-www-form-urlencoded"
	GZipCompressedArchive MIME = "application/gzip"
	GIF                   MIME = "image/gif"
	HTML                  MIME = "text/html"
	ICalendar             MIME = "text/calendar"
	IconFormat            MIME = "image/vnd.microsoft.icon"
	JavaArchive           MIME = "application/java-archive"
	JavaScript            MIME = "text/javascript"

	// Deprecated: JavaScript covers this media type; this constant is an alias.
	JavaScriptModule         MIME = "text/javascript"
	JPEG                     MIME = "image/jpeg"
	JSON                     MIME = "application/json"
	JSONLD                   MIME = "application/ld+json"
	MIDI                     MIME = "audio/midi"
	MSEmbeddedOpenTypeFonts  MIME = "application/vnd.ms-fontobject"
	MSExcel                  MIME = "application/vnd.ms-excel"
	MSExcelOpenXML           MIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	MSPowerPoint             MIME = "application/vnd.ms-powerpoint"
	MSPowerPointOpenXML      MIME = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	MSVisio                  MIME = "application/vnd.visio"
	MSWord                   MIME = "application/msword"
	MSWordOpenXML            MIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	MP3Audio                 MIME = "audio/mpeg"
	MP4Video                 MIME = "video/mp4"
	MPEGTransportStream      MIME = "video/mp2t"
	MPEGVideo                MIME = "video/mpeg"
	MultipartFormData        MIME = "multipart/form-data"
	OGG                      MIME = "application/ogg"
	OGGAudio                 MIME = "audio/ogg"
	OGGVideo                 MIME = "video/ogg"
	OpusAudio                MIME = "audio/opus"
	OpenDocumentPresentation MIME = "application/vnd.oasis.opendocument.presentation"
	OpenDocumentSpreadsheet  MIME = "application/vnd.oasis.opendocument.spreadsheet"
	OpenDocumentText         MIME = "application/vnd.oasis.opendocument.text"
	OpenTypeFont             MIME = "font/otf"
	PDF                      MIME = "application/pdf"
	PHP                      MIME = "application/x-httpd-php"
	PNG                      MIME = "image/png"
	RARArchive               MIME = "application/vnd.rar"
	RichTextFormat           MIME = "application/rtf"
	SevenZipArchive          MIME = "application/x-7z-compressed"
	SVG                      MIME = "image/svg+xml"
	TARArchive               MIME = "application/x-tar"
	Text                     MIME = "text/plain"
	ThreeG2AudioVideo        MIME = "video/3gpp2"
	ThreeGPAudioVideo        MIME = "video/3gpp"
	TIFF                     MIME = "image/tiff"
	TrueTypeFont             MIME = "font/ttf"
	WAVAudio                 MIME = "audio/wav"
	WEBMAudio                MIME = "audio/webm"
	WEBMVideo                MIME = "video/webm"
	WEBPImage                MIME = "image/webp"
	WOFF                     MIME = "font/woff"
	WOFF2                    MIME = "font/woff2"
	XML                      MIME = "application/xml"
	XHTML                    MIME = "application/xhtml+xml"
	XUL                      MIME = "application/vnd.mozilla.xul+xml"
	ZIPArchive               MIME = "application/zip"
)
