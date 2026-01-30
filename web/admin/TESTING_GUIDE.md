# Service Worker Update Handling - Testing Guide

## Quick Verification

The implementation is complete and the build is successful. Here's how to test the new update notification feature.

## Local Testing (Recommended)

### 1. Initial Build
```bash
cd /home/richard/GIT/OsmDeviceAdapter/web/admin
npm run build
npm run preview
```

### 2. Make a Visible Change

Edit a file to make an obvious visual change, for example:

**File: `src/ui/App.tsx` (line 82)**
```typescript
<h1>Patrol Scores Admin</h1>
```

Change to:
```typescript
<h1>Patrol Scores Admin - Version 2</h1>
```

### 3. Rebuild Without Closing Preview
```bash
npm run build
```

Keep the preview server running from step 1.

### 4. Wait and Watch

1. **In the browser tab showing the app**, wait approximately 30 seconds
2. The vite-plugin-pwa will poll for updates (default interval)
3. **You should see**: A banner appear at the top with:
   - Text: "A new version is available"
   - Two buttons: "Update now" and "Later"

### 5. Test "Later" Button

1. Click the **"Later"** button
2. **Expected**: Banner disappears
3. **State**: Update is available but dismissed
4. **Note**: Banner won't reappear until the page is reloaded or a newer version is detected

### 6. Test "Update now" Button

To test this, repeat steps 2-4 to trigger another update, then:

1. Click the **"Update now"** button
2. **Expected**: Page reloads immediately
3. **Expected**: New version appears (e.g., "Patrol Scores Admin - Version 2")
4. **Expected**: Banner disappears (no update available anymore)

## What to Look For

### ✅ Success Indicators

1. **Banner appears**: Update prompt shows at top of screen
2. **Banner styling**: White text on blue background with two buttons
3. **"Later" works**: Banner disappears, can continue using app
4. **"Update now" works**: Page reloads, new code appears
5. **No console errors**: Check browser DevTools console
6. **Service Worker active**: Check Application → Service Workers in DevTools

### ❌ Failure Indicators

1. **Banner never appears**: Check console for errors, verify SW is registered
2. **"Update now" doesn't reload**: Check console, verify updateSW function exists
3. **"Later" doesn't hide**: Check Redux state in Redux DevTools
4. **Page reloads but old code shows**: Check SW activation status

## DevTools Inspection

### Service Worker State

1. Open **Chrome DevTools** (F12)
2. Go to **Application** tab
3. Click **Service Workers** in left sidebar
4. You should see:
   - Current SW: "activated and is running"
   - When update available: "waiting to activate"

### Redux State

If you have Redux DevTools installed:

1. Open Redux DevTools
2. Look for `app` state:
   ```json
   {
     "app": {
       "updateAvailable": true,
       "updateDismissed": false
     }
   }
   ```

### Console Messages

Look for these log messages:
- `"New service worker version available"` - When update detected
- `"App is ready to work offline"` - When SW is ready

## Production Testing

### After Deployment

1. **Visit the app** in a browser
2. **Make a code change** and deploy a new version
3. **Wait for Kubernetes** to pick up the new version (pod restart)
4. **In the browser** (without reloading), wait ~30 seconds
5. **Banner should appear** prompting for update

### Cloudflare Tunnel Consideration

Since the app is deployed behind Cloudflare Tunnel:
- Service worker updates work through the tunnel
- Browser will detect new SW version from the CDN
- No special configuration needed for Cloudflare

## Troubleshooting

### Update Prompt Doesn't Appear

**Check 1: Service Worker Registration**
```javascript
// In browser console:
navigator.serviceWorker.getRegistrations().then(regs => console.log(regs))
```
Should show at least one registration.

**Check 2: Vite Config**
Verify `vite.config.ts` has:
```typescript
registerType: 'prompt'
```

**Check 3: Browser Support**
Service Workers require HTTPS (except localhost). Check:
```javascript
// In browser console:
console.log('SW supported:', 'serviceWorker' in navigator)
```

### Update Prompt Appears but "Update now" Doesn't Work

**Check 1: updateSW Function**
```javascript
// In browser console:
console.log(window.__updateServiceWorker)
```
Should be a function.

**Check 2: Console Errors**
Look for errors when clicking the button.

**Check 3: Service Worker State**
In DevTools → Application → Service Workers:
- Should show "waiting to activate" before clicking
- Should show "activated" after clicking

### Page Reloads but Old Code Shows

**Check 1: Hard Refresh**
Try Ctrl+Shift+R (or Cmd+Shift+R on Mac) to force cache clear.

**Check 2: Service Worker Activation**
In DevTools → Application → Service Workers:
- Verify new SW is "activated and is running"
- Check if old SW is still listed (shouldn't be)

**Check 3: Cache**
In DevTools → Application → Cache Storage:
- Clear all caches and try again

## Testing Checklist

- [ ] Banner appears when update is available
- [ ] "Later" button hides the banner
- [ ] "Update now" button reloads the page
- [ ] New code appears after reload
- [ ] No console errors during the process
- [ ] Service worker shows correct state in DevTools
- [ ] Redux state updates correctly
- [ ] Multiple tabs show banner (optional: test multi-tab behavior)
- [ ] Banner reappears after next update (dismissal resets)

## Next Steps

After verifying the update handling works:

1. **Deploy to staging** (if available) and test with real users
2. **Monitor** for any issues or unexpected behavior
3. **Collect feedback** on UX (banner placement, timing, wording)
4. **Consider enhancements** (see UPDATE_HANDLING_SUMMARY.md)

## Getting Help

If issues occur:
1. Check browser console for errors
2. Review `IMPLEMENTATION_NOTES.md` for architecture details
3. Verify service worker state in DevTools
4. Check Redux state in Redux DevTools
5. Review vite-plugin-pwa documentation: https://vite-pwa-org.netlify.app/

## Files to Reference

- `UPDATE_HANDLING_SUMMARY.md` - High-level overview and design decisions
- `IMPLEMENTATION_NOTES.md` - Detailed technical documentation
- `src/ui/components/UpdatePrompt.tsx` - Banner component
- `src/ui/hooks/useServiceWorkerUpdates.ts` - Update detection hook
- `src/ui/state/appSlice.ts` - Redux state management
