# HTML Template Refactoring Documentation

**Created**: 2026-01-13
**Status**: Technical Debt / Future Improvement

## Executive Summary

The OAuth web flow currently inlines ~372 lines of HTML/CSS across 7 page templates in `internal/handlers/oauth_web.go`. This was appropriate for initial development but now creates maintenance burden through CSS duplication and readability issues. This document outlines the current state and recommended refactoring approaches.

## Current State Analysis

### Single File Impact
All HTML/CSS is concentrated in one file: `internal/handlers/oauth_web.go` (816 lines total)

### HTML Rendering Blocks

| Line Range | Function | Purpose | Size | Complexity |
|-----------|----------|---------|------|------------|
| 25-46 | `OAuthAuthorizeHandler` | Device authorization input form | 22 lines | Simple form |
| 86-102 | `OAuthAuthorizeHandler` | Rate limit warning page | 17 lines | Static warning |
| 303-348 | `OAuthCancelHandler` | Authorization cancelled page | 46 lines | Static message |
| 366-374 | `OAuthCallbackHandler` | Authorization denied page | 9 lines | Error message |
| 473-489 | `OAuthSelectSectionHandler` | Authorization successful page | 17 lines | Success message |
| 553-735 | `showDeviceConfirmationPage` | Device confirmation with metadata | **183 lines** | Complex table, conditional display |
| 756-815 | `showSectionSelectionPage` | Scout section selection form | 78 lines | Dynamic form with options |

**Total**: ~372 lines of inlined HTML/CSS

### Current Technical Approach

**Pattern**: Raw string formatting with `fmt.Fprintf(w, `...`)`
- No template engine (`html/template` not used)
- No external template files
- All HTML embedded as Go string literals using backticks
- Dynamic content via `fmt.Sprintf()` and conditional string building
- Security: User data properly escaped with `html.EscapeString()`

### CSS Analysis

**Inline CSS Only** - No external stylesheets

**Duplication Issues**:
- Button styles repeated in 4+ blocks
- Form styling duplicated across 3+ pages
- Color scheme (success/error/warning) defined multiple times
- Responsive media queries duplicated

**Common Patterns**:
```css
/* Repeated across multiple blocks */
- Background colors: #f9f9f9, #fff
- Success green: #28a745
- Error red: #dc3545
- Warning yellow: #ffc107
- Border radius: 8px
- Box shadows: 0 2px 4px rgba(0,0,0,0.1)
- Font: Arial, sans-serif
- Mobile breakpoint: max-width 600px
```

**Estimated CSS Rules**: ~60+ declarations across ~25 classes

## Problems Identified

### 1. CSS Duplication
**Impact**: Maintenance burden, inconsistency risk
Button styles, form layouts, and color schemes repeated across 4-5 templates. Changing the UI theme requires editing multiple string literals scattered through the file.

### 2. Readability
**Impact**: Code review difficulty, developer experience
The largest HTML block (183 lines) interrupts Go logic flow. Reviewing changes requires parsing HTML within Go strings with escape sequences.

### 3. Maintainability
**Impact**: Change velocity, error proneness
Making UI changes requires:
- Finding all occurrences of duplicated CSS
- Careful string literal editing (syntax errors not caught until runtime)
- Testing entire flow to verify changes
- Risk of introducing HTML syntax errors

### 4. Collaboration
**Impact**: Team efficiency
UI designers or frontend developers cannot easily modify pages without Go knowledge. No standard tooling (HTML linters, formatters) works on embedded strings.

### 5. Testing
**Impact**: Test coverage
Cannot unit test HTML rendering separately from HTTP handlers. Template logic (conditional rendering, loops) embedded in handler functions.

### 6. Localization
**Impact**: Future i18n support
All text is hardcoded in Go strings. Adding multi-language support would require significant refactoring.

## When Inline HTML Makes Sense

This approach **was appropriate** during initial development:
- ✅ Simple error pages (1-10 lines)
- ✅ Very dynamic content where logic/template boundary is unclear
- ✅ Early prototypes and MVP phase
- ✅ Single-file simplicity for deployment

## When to Refactor (Current State)

You should refactor when you have:
- ❌ 7+ separate page templates (you have 7)
- ❌ Significant CSS duplication (button styles 4x, forms 3x)
- ❌ HTML blocks over 100 lines (you have 183-line block)
- ❌ Form validation and complex UI logic (device confirmation page)
- ❌ Maintenance burden outweighs simplicity benefit

**Verdict**: You've crossed the threshold where refactoring provides value.

## Recommended Refactoring Options

### Option 1: Go `html/template` with Embedded Templates (RECOMMENDED)

**Best for**: Go projects prioritizing type safety, deployment simplicity, Go-native tooling

#### Architecture
```
internal/templates/
  ├── templates.go           # Embedded filesystem, template loading
  ├── base.html              # Shared CSS, header, footer structure
  ├── device-auth.html       # Device authorization form
  ├── device-confirm.html    # Device confirmation with metadata table
  ├── section-select.html    # Scout section selection form
  ├── auth-cancelled.html    # Authorization cancelled message
  ├── auth-denied.html       # Authorization denied message
  ├── auth-success.html      # Authorization successful message
  └── rate-limited.html      # Rate limit warning
```

#### Implementation Approach

**Step 1**: Extract shared CSS into base template
```html
<!-- base.html -->
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <style>
        /* Shared CSS extracted from all pages */
        body { font-family: Arial, sans-serif; margin: 0; padding: 20px; background: #f9f9f9; }
        .container { max-width: 600px; margin: 0 auto; background: white; padding: 30px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .btn { padding: 12px 24px; font-size: 16px; border: none; border-radius: 4px; cursor: pointer; }
        .btn-primary { background: #007bff; color: white; }
        .btn-success { background: #28a745; color: white; }
        .btn-danger { background: #dc3545; color: white; }
        /* ... more shared styles ... */
        @media (max-width: 600px) {
            .container { padding: 20px; }
        }
    </style>
</head>
<body>
    <div class="container">
        {{block "content" .}}{{end}}
    </div>
</body>
</html>
```

**Step 2**: Create page-specific templates
```html
<!-- device-auth.html -->
{{template "base" .}}
{{define "content"}}
    <h1>Device Authorization</h1>
    <p>Enter the code displayed on your device:</p>
    <form method="POST" action="/oauth/authorize">
        <input type="hidden" name="session_id" value="{{.SessionID}}">
        <input type="text" name="user_code" placeholder="XXXX-XXXX" required
               pattern="[A-Z0-9]{4}-[A-Z0-9]{4}" style="...">
        <button type="submit" class="btn btn-primary">Continue</button>
    </form>
{{end}}
```

**Step 3**: Template loading with `embed`
```go
// internal/templates/templates.go
package templates

import (
    "embed"
    "html/template"
    "io"
)

//go:embed *.html
var templateFS embed.FS

var templates *template.Template

func init() {
    var err error
    templates, err = template.ParseFS(templateFS, "*.html")
    if err != nil {
        panic(err)
    }
}

func Render(w io.Writer, name string, data interface{}) error {
    return templates.ExecuteTemplate(w, name, data)
}
```

**Step 4**: Update handlers
```go
// internal/handlers/oauth_web.go
func (d *Dependencies) OAuthAuthorizeHandler(w http.ResponseWriter, r *http.Request) {
    // ... existing logic ...

    data := struct {
        Title     string
        SessionID string
    }{
        Title:     "Device Authorization",
        SessionID: sessionID,
    }

    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    if err := templates.Render(w, "device-auth.html", data); err != nil {
        slog.Error("template render failed", "error", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
    }
}
```

#### Benefits
- **Type-safe**: Compile-time template parsing catches syntax errors
- **Cached**: Templates parsed once at startup, not per-request
- **Secure**: Automatic context-aware HTML escaping (better than manual `html.EscapeString()`)
- **Native**: Standard library solution, zero external dependencies
- **Embedded**: Templates compiled into binary, no runtime file dependencies
- **Testable**: Can unit test template rendering independently
- **Maintainable**: ~60 lines of shared CSS instead of ~200+ duplicated

#### Tradeoffs
- Requires refactoring all 7 handlers (~2-3 hours work)
- Slightly more complex build (but `embed` is standard)
- Template syntax learning curve (minimal for team already using Go)

### Option 2: Extract CSS, Keep Minimal Templates in Go

**Best for**: Quick wins, minimal refactoring effort, preserving current architecture

#### Approach
```go
// internal/templates/styles.go
package templates

const BaseStyles = `
    body { font-family: Arial, sans-serif; margin: 0; padding: 20px; background: #f9f9f9; }
    .container { max-width: 600px; margin: 0 auto; background: white; padding: 30px; border-radius: 8px; }
    /* ... all shared CSS ... */
`

// Helper functions for common HTML patterns
func RenderButton(label, class, onclick string) string {
    return fmt.Sprintf(`<button class="%s" onclick="%s">%s</button>`, class, onclick, label)
}

func RenderFormHeader(title, description string) string {
    return fmt.Sprintf(`
        <h1>%s</h1>
        <p>%s</p>
    `, html.EscapeString(title), html.EscapeString(description))
}
```

Then in handlers:
```go
fmt.Fprintf(w, `<!DOCTYPE html><html><head><style>%s</style></head><body>`, templates.BaseStyles)
fmt.Fprintf(w, templates.RenderFormHeader("Device Authorization", "Enter your code"))
// ... rest of form ...
```

#### Benefits
- Minimal changes to existing code
- Eliminates CSS duplication immediately
- Can be done incrementally (one page at a time)
- No new dependencies or build complexity

#### Tradeoffs
- Still mixing HTML with Go logic
- Limited improvement to readability
- Doesn't address testing or collaboration issues

### Option 3: External Templates with `html/template`

**Best for**: Designer collaboration, frequent UI iteration, separate UI/backend teams

#### Architecture
```
templates/
  ├── base.html
  ├── device-auth.html
  └── ...

Dockerfile:
COPY templates/ /app/templates/
```

#### Loading
```go
// Load from filesystem at runtime
templates := template.Must(template.ParseGlob("templates/*.html"))
```

#### Benefits
- Designers can edit HTML without recompiling
- Standard HTML tooling (linters, formatters, IDE support)
- Hot-reload during development
- Clear separation of concerns

#### Tradeoffs
- Runtime file dependencies (templates must exist on disk)
- Cannot embed in single binary without `embed` package (negating the benefit)
- Deployment complexity (must copy templates to container/server)
- File I/O on every template load (unless cached)

**Note**: If using `embed` anyway, Option 1 is strictly better than Option 3.

### Option 4: Modern Template Framework (templ, gomponents)

**Best for**: New projects, teams wanting type-safe HTML-in-Go, advanced UI requirements

#### Example with [templ](https://templ.guide/)
```templ
// internal/templates/device_auth.templ
package templates

templ DeviceAuth(sessionID string) {
    @base("Device Authorization") {
        <h1>Device Authorization</h1>
        <form method="POST" action="/oauth/authorize">
            <input type="hidden" name="session_id" value={sessionID}/>
            <input type="text" name="user_code" placeholder="XXXX-XXXX" required/>
            <button type="submit" class="btn btn-primary">Continue</button>
        </form>
    }
}
```

#### Benefits
- Type-safe props (compile-time errors for missing data)
- IDE autocomplete for template data
- Component composition
- Very fast rendering
- Modern developer experience

#### Tradeoffs
- Requires code generation step in build
- External dependency (templ tool)
- Learning curve (new syntax, new tooling)
- **Not recommended for this project** (overkill for 7 pages)

## Recommendation: Option 1 (html/template with embed)

### Why Option 1?

**Best fit for this project**:
- Go-native solution, no external dependencies
- Single binary deployment (Kubernetes friendly)
- Significantly improves maintainability
- Standard approach in Go community
- Security improvement (automatic escaping)
- Future-proof for i18n, theming, etc.

**Effort estimation**: 2-3 hours
- Extract CSS: 30 minutes
- Create 7 templates: 60 minutes
- Update handlers: 45 minutes
- Testing: 30 minutes

### Implementation Checklist

```
[ ] 1. Create internal/templates/ directory
[ ] 2. Extract shared CSS into base.html
[ ] 3. Create 7 page-specific templates:
    [ ] device-auth.html
    [ ] device-confirm.html
    [ ] section-select.html
    [ ] auth-cancelled.html
    [ ] auth-denied.html
    [ ] auth-success.html
    [ ] rate-limited.html
[ ] 4. Create templates.go with embed and Render()
[ ] 5. Define data structs for each template
[ ] 6. Update OAuthAuthorizeHandler
[ ] 7. Update OAuthCallbackHandler
[ ] 8. Update OAuthCancelHandler
[ ] 9. Update OAuthSelectSectionHandler
[ ] 10. Update showDeviceConfirmationPage
[ ] 11. Update showSectionSelectionPage
[ ] 12. Test OAuth flow end-to-end
[ ] 13. Verify HTML escaping works correctly
[ ] 14. Check mobile responsive behavior
[ ] 15. Update this document with "COMPLETED" status
```

## Migration Strategy

### Phase 1: Infrastructure (No User Impact)
1. Create `internal/templates/` package
2. Extract CSS into base.html
3. Set up template loading with `embed`
4. Create helper `Render()` function

### Phase 2: Incremental Migration
Convert one page at a time, test thoroughly:
1. Start with simplest page (auth-denied.html - 9 lines)
2. Test in staging environment
3. Convert next page, repeat
4. Leave most complex page (device-confirm) for last

### Phase 3: Cleanup
1. Remove old HTML strings from oauth_web.go
2. Update documentation
3. Consider adding template unit tests

### Rollback Plan
Keep old code commented out during migration. If issues arise, uncomment and redeploy.

## Testing Considerations

### Current State
- HTML rendering tested only through full integration tests
- No way to verify HTML structure without running HTTP server

### After Refactoring
```go
// internal/templates/templates_test.go
func TestDeviceAuthTemplate(t *testing.T) {
    var buf bytes.Buffer
    data := DeviceAuthData{SessionID: "test-session"}

    err := Render(&buf, "device-auth.html", data)
    assert.NoError(t, err)

    html := buf.String()
    assert.Contains(t, html, "Device Authorization")
    assert.Contains(t, html, "test-session")
    assert.Contains(t, html, `name="user_code"`)
}
```

## Future Enhancements Enabled

Once templates are extracted, these become easier:

1. **Internationalization (i18n)**
   - Add translation function to template context
   - Extract strings to message catalogs

2. **Theming**
   - Allow CSS variables for colors
   - Dark mode support

3. **A/B Testing**
   - Swap templates based on feature flags
   - Test different UI flows

4. **Accessibility**
   - Easier to audit and improve ARIA labels
   - Screen reader testing

5. **Performance**
   - Template caching (already enabled by default)
   - Precompute static parts

## Security Considerations

### Current: Manual Escaping
```go
deviceIP = html.EscapeString(*deviceCode.DeviceRequestIP)
currentIP := html.EscapeString(currentMetadata.IP)
```

### After: Automatic Escaping
```html
<!-- html/template escapes by default -->
<p>Device IP: {{.DeviceIP}}</p>
<p>Current IP: {{.CurrentIP}}</p>
```

**Important**: `html/template` provides context-aware escaping:
- HTML content: HTML-escaped
- HTML attributes: Attribute-escaped
- JavaScript: JavaScript-escaped
- CSS: CSS-escaped
- URLs: URL-escaped

This is **more secure** than manual `html.EscapeString()` which only handles HTML context.

### Unsafe HTML (Rare Cases)
If you need to render trusted HTML (e.g., from admin-provided content):
```html
{{.TrustedHTML | safeHTML}}
```
**Warning**: Only use for known-safe content, never user input.

## References

- [Go html/template docs](https://pkg.go.dev/html/template)
- [Go embed package](https://pkg.go.dev/embed)
- [Template best practices](https://www.calhoun.io/intro-to-templates-p3-functions/)
- OSM Device Adapter: `internal/handlers/oauth_web.go` (current implementation)

## Related Documentation

- `README.md` - User-facing documentation
- `docs/security.md` - Security architecture (XSS prevention relates to template escaping)
- `CLAUDE.md` - Code architecture (add templates/ to key components after refactoring)

## Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-01-13 | Document current state | Preserve analysis before 5-hour session limit |
| TBD | Choose refactoring option | Pending team discussion |
| TBD | Implementation started | - |
| TBD | Migration completed | - |

## Questions for Team Discussion

1. **Timeline**: Should this be done before next feature, or is current approach acceptable?
2. **Designer involvement**: Will non-Go developers need to edit UI in the future?
3. **Branding**: Are UI changes expected (logo, colors, theme)?
4. **Mobile**: Is mobile responsiveness a priority? (Current CSS has basic responsive design)
5. **Accessibility**: Are WCAG compliance or screen reader support requirements?
6. **Localization**: Will multi-language support be needed?

## Conclusion

The current inline HTML approach served well during initial development but has reached the point where refactoring provides clear value. **Option 1 (html/template with embed)** is recommended as the best balance of:
- Maintainability improvement
- Security enhancement
- Go ecosystem alignment
- Deployment simplicity
- Reasonable implementation effort

The refactoring can be done incrementally with minimal risk and will pay dividends in future UI maintenance and enhancement work.
