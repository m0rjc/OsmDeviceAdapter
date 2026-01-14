package templates

import (
	"embed"
	"html/template"
	"io"
	"log/slog"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

//go:embed *.html
var templateFS embed.FS

var templates *template.Template

func init() {
	var err error
	templates, err = template.New("").ParseFS(templateFS, "*.html")
	if err != nil {
		panic(err)
	}
}

// Render executes a template with the given data and writes to w
func Render(w io.Writer, name string, data interface{}) error {
	// Create a clone of the template set to avoid race conditions
	t, err := templates.Clone()
	if err != nil {
		return err
	}

	// Extract template name from filename (e.g., "device-auth.html" -> "device-auth")
	templateName := name
	if len(templateName) > 5 && templateName[len(templateName)-5:] == ".html" {
		templateName = templateName[:len(templateName)-5]
	}

	// Create a bridge template that defines "content" to invoke our specific template
	// This allows base.html's {{template "content" .}} to call the correct template
	bridge := `{{define "content"}}{{template "` + templateName + `" .}}{{end}}`
	t, err = t.New("bridge").Parse(bridge)
	if err != nil {
		return err
	}

	// Execute the base template which will include our specific content via the bridge
	return t.ExecuteTemplate(w, "base.html", data)
}

// DeviceAuthData is the data structure for the device authorization form
type DeviceAuthData struct {
	Title string
}

// RateLimitedData is the data structure for the rate limited page
type RateLimitedData struct {
	Title        string
	RetrySeconds int
}

// AuthDeniedData is the data structure for the authorization denied page
type AuthDeniedData struct {
	Title string
}

// AuthCancelledData is the data structure for the authorization cancelled page
type AuthCancelledData struct {
	Title string
}

// AuthSuccessData is the data structure for the authorization success page
type AuthSuccessData struct {
	Title string
}

// DeviceConfirmData is the data structure for the device confirmation page
type DeviceConfirmData struct {
	Title              string
	UserCode           string
	DeviceIP           string
	DeviceCountry      string
	DeviceTime         string
	CurrentIP          string
	CurrentCountry     string
	ShowCountryWarning bool
	SessionID          string
}

// SectionSelectData is the data structure for the section selection page
type SectionSelectData struct {
	Title     string
	SessionID string
	Sections  []types.OSMSection
}

// RenderDeviceAuth renders the device authorization form
func RenderDeviceAuth(w io.Writer) error {
	data := DeviceAuthData{
		Title: "Device Authorization",
	}
	return Render(w, "device-auth.html", data)
}

// RenderRateLimited renders the rate limited page
func RenderRateLimited(w io.Writer, retrySeconds int) error {
	data := RateLimitedData{
		Title:        "Please Slow Down",
		RetrySeconds: retrySeconds,
	}
	return Render(w, "rate-limited.html", data)
}

// RenderAuthDenied renders the authorization denied page
func RenderAuthDenied(w io.Writer) error {
	data := AuthDeniedData{
		Title: "Authorization Denied",
	}
	return Render(w, "auth-denied.html", data)
}

// RenderAuthCancelled renders the authorization cancelled page
func RenderAuthCancelled(w io.Writer) error {
	data := AuthCancelledData{
		Title: "Authorization Cancelled",
	}
	return Render(w, "auth-cancelled.html", data)
}

// RenderAuthSuccess renders the authorization success page
func RenderAuthSuccess(w io.Writer) error {
	data := AuthSuccessData{
		Title: "Authorization Successful",
	}
	return Render(w, "auth-success.html", data)
}

// RenderDeviceConfirm renders the device confirmation page
func RenderDeviceConfirm(w io.Writer, userCode, deviceIP, deviceCountry, deviceTime, currentIP, currentCountry, sessionID string, showCountryWarning bool) error {
	data := DeviceConfirmData{
		Title:              "Confirm Device Authorization",
		UserCode:           userCode,
		DeviceIP:           deviceIP,
		DeviceCountry:      deviceCountry,
		DeviceTime:         deviceTime,
		CurrentIP:          currentIP,
		CurrentCountry:     currentCountry,
		ShowCountryWarning: showCountryWarning,
		SessionID:          sessionID,
	}
	return Render(w, "device-confirm.html", data)
}

// RenderSectionSelect renders the section selection page
func RenderSectionSelect(w io.Writer, sessionID string, sections []types.OSMSection) error {
	data := SectionSelectData{
		Title:     "Select Scout Section",
		SessionID: sessionID,
		Sections:  sections,
	}
	slog.Debug("templates.render.section_select",
		"component", "templates",
		"event", "render.section_select",
		"session_id", sessionID,
		"sections_count", len(sections),
	)
	return Render(w, "section-select.html", data)
}
