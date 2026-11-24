package httpclient

// MIME represents an Internet Media Type (content type). Conversion from any
// string is allowed; the type does not validate values. The constants cover
// common registered and de facto media types, ordered by name.
type MIME string

// String returns the underlying string representation of the MIME type.
func (m MIME) String() (mime string) {
	mime = string(m)

	return mime
}

// Predefined MIME type constants.
const (
	MIMEAACAudio              MIME = "audio/aac"
	MIMEAbiWordDocument       MIME = "application/x-abiword"
	MIMEAmazonKindleEBook     MIME = "application/vnd.amazon.ebook"
	MIMEAppleInstallerPackage MIME = "application/vnd.apple.installer+xml"
	MIMEArchiveDocument       MIME = "application/x-freearc"
	MIMEAVIFImage             MIME = "image/avif"
	MIMEAVIVideo              MIME = "video/x-msvideo"
	MIMEBinaryData            MIME = "application/octet-stream"
	MIMEBitmapImage           MIME = "image/bmp"
	MIMEBourneShellScript     MIME = "application/x-sh"
	MIMEBZip2Archive          MIME = "application/x-bzip2"
	MIMEBZipArchive           MIME = "application/x-bzip"
	MIMECDAudio               MIME = "application/x-cdf"
	MIMECShellScript          MIME = "application/x-csh"
	MIMECSS                   MIME = "text/css"
	MIMECSV                   MIME = "text/csv"
	MIMEEPUB                  MIME = "application/epub+zip"
	MIMEEventStream           MIME = "text/event-stream"
	MIMEFormURLEncoded        MIME = "application/x-www-form-urlencoded"
	MIMEGZipCompressedArchive MIME = "application/gzip"
	MIMEGIF                   MIME = "image/gif"
	MIMEHTML                  MIME = "text/html"
	MIMEICalendar             MIME = "text/calendar"
	MIMEIconFormat            MIME = "image/vnd.microsoft.icon"
	MIMEJavaArchive           MIME = "application/java-archive"
	MIMEJavaScript            MIME = "text/javascript"

	// Deprecated: MIMEJavaScript covers this media type; this constant is an alias.
	MIMEJavaScriptModule         MIME = "text/javascript"
	MIMEJPEG                     MIME = "image/jpeg"
	MIMEJSON                     MIME = "application/json"
	MIMEJSONLD                   MIME = "application/ld+json"
	MIMEMIDI                     MIME = "audio/midi"
	MIMEMSEmbeddedOpenTypeFonts  MIME = "application/vnd.ms-fontobject"
	MIMEMSExcel                  MIME = "application/vnd.ms-excel"
	MIMEMSExcelOpenXML           MIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	MIMEMSPowerPoint             MIME = "application/vnd.ms-powerpoint"
	MIMEMSPowerPointOpenXML      MIME = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	MIMEMSVisio                  MIME = "application/vnd.visio"
	MIMEMSWord                   MIME = "application/msword"
	MIMEMSWordOpenXML            MIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	MIMEMP3Audio                 MIME = "audio/mpeg"
	MIMEMP4Video                 MIME = "video/mp4"
	MIMEMPEGTransportStream      MIME = "video/mp2t"
	MIMEMPEGVideo                MIME = "video/mpeg"
	MIMEMultipartFormData        MIME = "multipart/form-data"
	MIMEOGG                      MIME = "application/ogg"
	MIMEOGGAudio                 MIME = "audio/ogg"
	MIMEOGGVideo                 MIME = "video/ogg"
	MIMEOpusAudio                MIME = "audio/opus"
	MIMEOpenDocumentPresentation MIME = "application/vnd.oasis.opendocument.presentation"
	MIMEOpenDocumentSpreadsheet  MIME = "application/vnd.oasis.opendocument.spreadsheet"
	MIMEOpenDocumentText         MIME = "application/vnd.oasis.opendocument.text"
	MIMEOpenTypeFont             MIME = "font/otf"
	MIMEPDF                      MIME = "application/pdf"
	MIMEPHP                      MIME = "application/x-httpd-php"
	MIMEPNG                      MIME = "image/png"
	MIMERARArchive               MIME = "application/vnd.rar"
	MIMERichTextFormat           MIME = "application/rtf"
	MIMESevenZipArchive          MIME = "application/x-7z-compressed"
	MIMESVG                      MIME = "image/svg+xml"
	MIMETARArchive               MIME = "application/x-tar"
	MIMEText                     MIME = "text/plain"
	MIMEThreeG2AudioVideo        MIME = "video/3gpp2"
	MIMEThreeGPAudioVideo        MIME = "video/3gpp"
	MIMETIFF                     MIME = "image/tiff"
	MIMETrueTypeFont             MIME = "font/ttf"
	MIMEWAVAudio                 MIME = "audio/wav"
	MIMEWEBMAudio                MIME = "audio/webm"
	MIMEWEBMVideo                MIME = "video/webm"
	MIMEWEBPImage                MIME = "image/webp"
	MIMEWOFF                     MIME = "font/woff"
	MIMEWOFF2                    MIME = "font/woff2"
	MIMEXML                      MIME = "application/xml"
	MIMEXHTML                    MIME = "application/xhtml+xml"
	MIMEXUL                      MIME = "application/vnd.mozilla.xul+xml"
	MIMEZIPArchive               MIME = "application/zip"
)
