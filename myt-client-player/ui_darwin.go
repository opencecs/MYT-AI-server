//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework Metal -framework MetalKit -framework QuartzCore -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <pthread.h>

#define EV_MOUSE_DOWN  0
#define EV_MOUSE_UP    1
#define EV_MOUSE_MOVE  2
#define EV_DBL_CLICK   3
#define EV_MOUSE_LEAVE 4
#define EV_KEY_DOWN    5
#define EV_KEY_UP      6
#define EV_SCROLL      7
#define EV_RESIZE      8
#define EV_NAV_BACK    10
#define EV_NAV_HOME    11
#define EV_NAV_RECENT  12
#define EV_OVERLAY_CLICK 13
#define EV_WIN_RESIZE 14
#define MAX_EVENTS 512

typedef struct {
    int evType;
    float x, y;
    int button, keycode, mods;
    unsigned int unicode;
    unsigned int textChar; // actual typed character from [e characters]
    float scrollDX, scrollDY;
    int width, height;
    int intArg;
    int buttonState; // mouse button state for move events
} DarwinEvent;

static DarwinEvent g_eventQ[MAX_EVENTS];
static int g_eventN = 0;
static pthread_mutex_t g_eventMtx = PTHREAD_MUTEX_INITIALIZER;
static NSWindow* playerWindow = nil;
static NSView* metalView = nil;
static NSView* navBarView = nil;
static WKWebView* sidebarWebView = nil;
static NSView* connOverlay = nil;
static NSView* uploadOverlay = nil;
static NSView* toastOverlay = nil;

// ─── Sidebar msg queue ──────────────────────────────────────────────────
static char g_sidebarMsg[8192] = {0};
static pthread_mutex_t g_sidebarMtx = PTHREAD_MUTEX_INITIALIZER;

static void storeSidebarMsg(const char* msg) {
    pthread_mutex_lock(&g_sidebarMtx);
    strncpy(g_sidebarMsg, msg, sizeof(g_sidebarMsg)-1);
    g_sidebarMsg[sizeof(g_sidebarMsg)-1] = '\0';
    pthread_mutex_unlock(&g_sidebarMtx);
}

int getSidebarMsg(char* out, int maxOut) {
    pthread_mutex_lock(&g_sidebarMtx);
    if (g_sidebarMsg[0] == '\0') { pthread_mutex_unlock(&g_sidebarMtx); return 0; }
    int len = (int)strlen(g_sidebarMsg);
    if (len >= maxOut) len = maxOut-1;
    memcpy(out, g_sidebarMsg, len); out[len] = '\0';
    g_sidebarMsg[0] = '\0';
    pthread_mutex_unlock(&g_sidebarMtx);
    return len;
}

// ─── Drop path queue ────────────────────────────────────────────────────
static char g_dropPath[2048] = {0};
static pthread_mutex_t g_dropMtx = PTHREAD_MUTEX_INITIALIZER;

static void storeDropPath(const char* p) {
    pthread_mutex_lock(&g_dropMtx);
    strncpy(g_dropPath, p, sizeof(g_dropPath)-1);
    g_dropPath[sizeof(g_dropPath)-1] = '\0';
    pthread_mutex_unlock(&g_dropMtx);
}

int getDropPath(char* out, int maxOut) {
    pthread_mutex_lock(&g_dropMtx);
    if (g_dropPath[0] == '\0') { pthread_mutex_unlock(&g_dropMtx); return 0; }
    int len = (int)strlen(g_dropPath);
    if (len >= maxOut) len = maxOut-1;
    memcpy(out, g_dropPath, len); out[len] = '\0';
    g_dropPath[0] = '\0';
    pthread_mutex_unlock(&g_dropMtx);
    return len;
}

// ─── Event push/drain ──────────────────────────────────────────────────
static void pushEvent(DarwinEvent ev) {
    pthread_mutex_lock(&g_eventMtx);
    if (g_eventN < MAX_EVENTS) g_eventQ[g_eventN++] = ev;
    pthread_mutex_unlock(&g_eventMtx);
}
int drainEvents(DarwinEvent* out, int maxOut) {
    pthread_mutex_lock(&g_eventMtx);
    int n = g_eventN; if (n > maxOut) n = maxOut;
    if (n > 0) { memcpy(out, g_eventQ, n*sizeof(DarwinEvent));
        if (g_eventN > n) memmove(g_eventQ, &g_eventQ[n], (g_eventN-n)*sizeof(DarwinEvent));
        g_eventN -= n;
    }
    pthread_mutex_unlock(&g_eventMtx);
    return n;
}

// ─── AppDelegate ────────────────────────────────────────────────────────
@interface AppDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate>
@property (nonatomic, assign) float aspectRatio;
@end
@implementation AppDelegate
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)s { (void)s; return YES; }
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)s { (void)s; return NSTerminateNow; }
- (BOOL)windowShouldClose:(NSWindow *)s { (void)s; return YES; }
- (NSSize)windowWillResize:(NSWindow *)sender toSize:(NSSize)frameSize {
    (void)sender;
    return frameSize;
}
- (void)windowDidResize:(NSNotification *)n {
    (void)n;
    if (playerWindow) {
        NSRect cr = [playerWindow contentRectForFrameRect:[playerWindow frame]];
        DarwinEvent ev={0}; ev.evType=14; ev.width=(int)cr.size.width; ev.height=(int)cr.size.height;
        pushEvent(ev);
    }
}
@end

// ─── MetalView ──────────────────────────────────────────────────────────
@interface MetalView : NSView {
    int _lastW, _lastH;
}
@end
@implementation MetalView
- (BOOL)isFlipped { return YES; }
- (BOOL)acceptsFirstResponder { return YES; }
- (BOOL)acceptsFirstMouse:(NSEvent *)e { (void)e; return YES; }
- (instancetype)initWithFrame:(NSRect)r {
    self = [super initWithFrame:r];
    if (self) {
        _lastW = -1; _lastH = -1;
        [self addTrackingArea:[[NSTrackingArea alloc]
            initWithRect:r options:NSTrackingMouseMoved|NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways|NSTrackingInVisibleRect
            owner:self userInfo:nil]];
        [self registerForDraggedTypes:@[NSPasteboardTypeFileURL]];
    }
    return self;
}
- (void)mouseDown:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=([e clickCount]>=2)?3:0; ev.x=p.x; ev.y=p.y; ev.button=1; pushEvent(ev); }
- (void)mouseUp:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=1; ev.x=p.x; ev.y=p.y; ev.button=1; pushEvent(ev); }
- (void)rightMouseDown:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=0; ev.x=p.x; ev.y=p.y; ev.button=3; pushEvent(ev); }
- (void)rightMouseUp:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=1; ev.x=p.x; ev.y=p.y; ev.button=3; pushEvent(ev); }
- (void)otherMouseDown:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=0; ev.x=p.x; ev.y=p.y; ev.button=2; pushEvent(ev); }
- (void)otherMouseUp:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=1; ev.x=p.x; ev.y=p.y; ev.button=2; pushEvent(ev); }
- (void)mouseMoved:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=2; ev.x=p.x; ev.y=p.y; ev.buttonState=0; pushEvent(ev); }
- (void)mouseExited:(NSEvent *)e { (void)e;
    DarwinEvent ev={0}; ev.evType=4; pushEvent(ev); }
- (void)mouseDragged:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=2; ev.x=p.x; ev.y=p.y;
    ev.buttonState = ([e buttonNumber]==0)?1:(([e buttonNumber]==1)?2:4); pushEvent(ev); }
- (void)rightMouseDragged:(NSEvent *)e { [self mouseDragged:e]; }
- (void)otherMouseDragged:(NSEvent *)e { [self mouseDragged:e]; }
- (void)scrollWheel:(NSEvent *)e { NSPoint p=[self convertPoint:[e locationInWindow] fromView:nil];
    DarwinEvent ev={0}; ev.evType=7; ev.x=p.x; ev.y=p.y; ev.scrollDY=[e scrollingDeltaY]; pushEvent(ev); }
// Override performKeyEquivalent: to intercept Cmd+C/X/V before the default
// Edit menu consumes them. Without this, the system menu bar (created
// automatically by NSApplicationActivationPolicyRegular) eats Cmd+C/X/V
// via its built-in Copy/Cut/Paste menu items, so keyDown: never fires.
- (BOOL)performKeyEquivalent:(NSEvent *)e {
    if ([e type] == NSEventTypeKeyDown) {
        NSUInteger f = [e modifierFlags] & NSEventModifierFlagDeviceIndependentFlagsMask;
        if (f & (NSEventModifierFlagCommand|NSEventModifierFlagControl)) {
            NSString *c = [e charactersIgnoringModifiers];
            if ([c length] > 0) {
                unichar ch = [c characterAtIndex:0];
                if (ch=='c'||ch=='C'||ch=='x'||ch=='X'||ch=='v'||ch=='V'||ch=='a'||ch=='A') {
                    [self keyDown:e];
                    return YES;
                }
            }
        }
    }
    return [super performKeyEquivalent:e];
}
- (void)keyDown:(NSEvent *)e {
    int mods=0; NSUInteger f=[e modifierFlags]&NSEventModifierFlagDeviceIndependentFlagsMask;
    if(f&NSEventModifierFlagShift) mods|=1;
    if(f&(NSEventModifierFlagControl|NSEventModifierFlagCommand)) mods|=0x40;
    if(f&NSEventModifierFlagOption) mods|=0x100;
    unsigned int uc=0; NSString *c=[e charactersIgnoringModifiers]; if([c length]>0) uc=[c characterAtIndex:0];
    unsigned int tc=0; NSString *ct=[e characters]; if([ct length]>0) tc=[ct characterAtIndex:0];
    DarwinEvent ev={0}; ev.evType=5; ev.keycode=[e keyCode]; ev.mods=mods; ev.unicode=uc; ev.textChar=tc; pushEvent(ev); }
- (void)keyUp:(NSEvent *)e {
    int mods=0; NSUInteger f=[e modifierFlags]&NSEventModifierFlagDeviceIndependentFlagsMask;
    if(f&NSEventModifierFlagShift) mods|=1;
    if(f&(NSEventModifierFlagControl|NSEventModifierFlagCommand)) mods|=0x40;
    if(f&NSEventModifierFlagOption) mods|=0x100;
    unsigned int uc=0; NSString *c=[e charactersIgnoringModifiers]; if([c length]>0) uc=[c characterAtIndex:0];
    DarwinEvent ev={0}; ev.evType=6; ev.keycode=[e keyCode]; ev.mods=mods; ev.unicode=uc; pushEvent(ev); }
- (void)setFrameSize:(NSSize)s {
    [super setFrameSize:s];
    int nw = (int)s.width, nh = (int)s.height;
    if (nw != _lastW || nh != _lastH) {
        _lastW = nw; _lastH = nh;
        DarwinEvent ev={0}; ev.evType=8; ev.width=nw; ev.height=nh; pushEvent(ev);
    }
}
// Drag and drop
- (NSDragOperation)draggingEntered:(id<NSDraggingInfo>)di { (void)di; return NSDragOperationCopy; }
- (NSDragOperation)draggingUpdated:(id<NSDraggingInfo>)di { (void)di; return NSDragOperationCopy; }
- (BOOL)performDragOperation:(id<NSDraggingInfo>)di {
    NSPasteboard *pb = [di draggingPasteboard];
    NSArray *items = [pb readObjectsForClasses:@[NSURL.class] options:nil];
    if ([items count] > 0) { NSString *p = [items[0] path]; storeDropPath([p UTF8String]); }
    return YES;
}
@end

// ─── SidebarHandler ────────────────────────────────────────────────────
@interface SidebarHandler : NSObject <WKScriptMessageHandler> @end
@implementation SidebarHandler
- (void)userContentController:(WKUserContentController *)cc didReceiveScriptMessage:(WKScriptMessage *)msg {
    (void)cc;
    NSString *body = [msg body];
    if ([body isKindOfClass:[NSString class]]) { storeSidebarMsg([body UTF8String]); }
}
@end

// ─── ClickableOverlay ──────────────────────────────────────────────────
@interface ClickableOverlay : NSView @property (nonatomic, assign) int clickTag; @end
@implementation ClickableOverlay
- (void)mouseDown:(NSEvent *)e { (void)e;
    DarwinEvent ev={0}; ev.evType=13; ev.intArg=self.clickTag; pushEvent(ev); }
@end

// ─── Nav pill button ─────────────────────────────────────────────────
@interface NavPillView : NSView {
    int _navTag;
    BOOL _hovered;
    BOOL _pressed;
}
@property (nonatomic, assign) int navTag;
- (void)drawRect:(NSRect)dirtyRect;
- (void)mouseDown:(NSEvent *)e;
- (void)mouseUp:(NSEvent *)e;
- (void)mouseEntered:(NSEvent *)e;
- (void)mouseExited:(NSEvent *)e;
- (void)updateTrackingAreas;
@end
@implementation NavPillView
- (int)navTag { return _navTag; }
- (void)setNavTag:(int)t { _navTag = t; }
- (BOOL)isFlipped { return YES; }
- (void)drawRect:(NSRect)dr {
    (void)dr;
    NSRect b = self.bounds;
    CGFloat w = b.size.width, h = b.size.height;
    // Pill background
    NSColor *bg = _pressed ? [NSColor colorWithCalibratedWhite:0.816 alpha:1.0]
               : _hovered ? [NSColor colorWithCalibratedWhite:0.878 alpha:1.0]
               : [NSColor clearColor];
    if (_hovered || _pressed) {
        NSBezierPath *pill = [NSBezierPath bezierPathWithRoundedRect:b xRadius:h/3 yRadius:h/3];
        [bg setFill]; [pill fill];
    }
    // Icon color
    NSColor *ic = _pressed || _hovered
        ? [NSColor colorWithCalibratedWhite:0.25 alpha:1.0]
        : [NSColor colorWithCalibratedWhite:0.44 alpha:1.0];
    CGFloat cx = w/2, cy = h/2, sz = h*0.28;
    CGFloat iconSz = h*0.28;
    if (_navTag == 0) {
        // Back: filled left triangle ◀
        CGFloat half = iconSz;
        NSPoint pts[3] = {
            {cx+half, cy-half}, {cx-half, cy}, {cx+half, cy+half}
        };
        NSBezierPath *tri = [NSBezierPath bezierPath];
        [tri moveToPoint:pts[0]]; [tri lineToPoint:pts[1]]; [tri lineToPoint:pts[2]]; [tri closePath];
        [ic setFill]; [tri fill];
    } else if (_navTag == 1) {
        // Home: thick circle ○
        CGFloat half = iconSz;
        NSRect cr = NSMakeRect(cx-half, cy-half, half*2, half*2);
        NSBezierPath *circ = [NSBezierPath bezierPathWithOvalInRect:cr];
        [ic setStroke]; [circ setLineWidth:3.0]; [circ stroke];
    } else {
        // Recent: thick rounded rect □
        CGFloat half = iconSz * 0.9;
        NSRect rr = NSMakeRect(cx-half, cy-half, half*2, half*2);
        NSBezierPath *rect = [NSBezierPath bezierPathWithRoundedRect:rr xRadius:4 yRadius:4];
        [ic setStroke]; [rect setLineWidth:3.0]; [rect stroke];
    }
}
- (void)mouseDown:(NSEvent *)e { (void)e; _pressed=YES; [self setNeedsDisplay:YES]; }
- (void)mouseUp:(NSEvent *)e { (void)e; _pressed=NO; [self setNeedsDisplay:YES];
    DarwinEvent ev={0}; ev.evType=10+_navTag; pushEvent(ev); }
- (void)mouseEntered:(NSEvent *)e { (void)e; _hovered=YES; [self setNeedsDisplay:YES]; }
- (void)mouseExited:(NSEvent *)e { (void)e; _hovered=NO; _pressed=NO; [self setNeedsDisplay:YES]; }
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    static NSTrackingArea *ta = nil;
    if (ta) [self removeTrackingArea:ta];
    ta = [[NSTrackingArea alloc] initWithRect:self.bounds
        options:NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways|NSTrackingInVisibleRect
        owner:self userInfo:nil];
    [self addTrackingArea:ta];
}
@end

// ─── Nav bar with auto-centering pills ───────────────────────────────────
@interface NavBarView : NSView
@end
@implementation NavBarView
- (BOOL)isFlipped { return YES; }
- (void)layoutPills {
    CGFloat w = self.frame.size.width, h = self.frame.size.height;
    CGFloat pillW = h*1.6;
    if (pillW > w/3) pillW = w/3;
    CGFloat pillH = h*0.72;
    CGFloat gap = h*0.7;
    CGFloat totalW = pillW*3 + gap*2;
    CGFloat pillY = (h - pillH)/2;
    CGFloat centerX = w/2;
    CGFloat left = centerX - totalW/2;
    CGFloat positions[3];
    positions[0] = left;
    positions[1] = left + pillW + gap;
    positions[2] = left + 2*(pillW + gap);
    int idx = 0;
    for (NSView *sv in self.subviews) {
        if ([sv isKindOfClass:[NavPillView class]]) {
            [sv setFrame:NSMakeRect(positions[idx], pillY, pillW, pillH)];
            [sv setNeedsDisplay:YES];
            idx++;
        }
    }
}
- (void)setFrameSize:(NSSize)newSize {
    [super setFrameSize:newSize];
    [self layoutPills];
}
@end

// ─── C API ──────────────────────────────────────────────────────────────
void createPlayerWindow(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        AppDelegate *del = [[AppDelegate alloc] init];
        [NSApp setDelegate:del];

        NSRect frame = NSMakeRect(0, 0, 464, 780);
        NSUInteger style = NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskMiniaturizable|NSWindowStyleMaskResizable;
        playerWindow = [[NSWindow alloc] initWithContentRect:frame styleMask:style backing:NSBackingStoreBuffered defer:NO];
        [playerWindow setTitle:@"MYT Player"];
        [playerWindow center];
        [playerWindow setDelegate:del];
        [playerWindow setMinSize:NSMakeSize(270, 480)];

        NSView *content = [playerWindow contentView];
        [content setWantsLayer:YES];
        content.layer.backgroundColor = [NSColor blackColor].CGColor;

        MetalView *mv = [[MetalView alloc] initWithFrame:content.bounds];
        [content addSubview:mv];
        metalView = mv;

        NavBarView *nbv = [[NavBarView alloc] initWithFrame:NSMakeRect(0,0,content.bounds.size.width,48)];
        [nbv setWantsLayer:YES];
        nbv.layer.backgroundColor = [NSColor colorWithCalibratedRed:0.94 green:0.94 blue:0.94 alpha:1.0].CGColor;

        for (int i=0; i<3; i++) {
            NavPillView *pv = [[NavPillView alloc] initWithFrame:NSMakeRect(0,0,1,1)];
            pv.navTag = i;
            [nbv addSubview:pv];
        }
        [nbv layoutPills];
        navBarView = nbv;
        [content addSubview:navBarView];

        [playerWindow makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
    }
}

void* getMetalView(void) { return metalView; }

void layoutViews(int winW, int winH, int sideW, int navH) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!metalView||!navBarView) return;
        int vw = winW - sideW, vh = winH;
        if (vw<0) vw=0; if (vh<0) vh=0;
        if (navH == 0 && sideW == 0) {
            [metalView setFrame:NSMakeRect(0, 0, vw, vh)];
            [navBarView setHidden:YES];
            if (sidebarWebView) { sidebarWebView.hidden = YES; }
        } else {
            // MetalView fills entire content area (NavBar floats on top at bottom)
            [metalView setFrame:NSMakeRect(0, 0, vw, vh)];
            [navBarView setHidden:NO];
            [navBarView setFrame:NSMakeRect(0, 0, vw, navH)];
            [(NavBarView*)navBarView layoutPills];
        }
        if (sidebarWebView) {
            [sidebarWebView setFrame:NSMakeRect(vw, 0, sideW, winH)];
            sidebarWebView.hidden = (sideW == 0);
        }
        // Reposition overlays to center of video area (excluding NavBar)
        CGFloat videoH = (navH > 0) ? (vh - navH) : vh;
        if (videoH < 0) videoH = 0;
        if (connOverlay) {
            CGFloat ow=connOverlay.bounds.size.width, oh=connOverlay.bounds.size.height;
            [connOverlay setFrame:NSMakeRect((vw-ow)/2, (videoH-oh)/2, ow, oh)];
        }
        if (uploadOverlay) {
            CGFloat ow=uploadOverlay.bounds.size.width, oh=uploadOverlay.bounds.size.height;
            [uploadOverlay setFrame:NSMakeRect((vw-ow)/2, (videoH-oh)/2, ow, oh)];
        }
        if (toastOverlay) {
            CGFloat ow=toastOverlay.bounds.size.width, oh=toastOverlay.bounds.size.height;
            [toastOverlay setFrame:NSMakeRect((vw-ow)/2, videoH/2-oh/2-60, ow, oh)];
        }
    });
}

void createSidebar(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (sidebarWebView) return;
        WKUserContentController *ucc = [[WKUserContentController alloc] init];
        SidebarHandler *sh = [[SidebarHandler alloc] init];
        [ucc addScriptMessageHandler:sh name:@"external"];
        NSString *bridge = @"if(!window.external){window.external={invoke:function(m){window.webkit.messageHandlers.external.postMessage(m);}};}"
            @"if(!window.chrome){window.chrome={webview:{postMessage:function(m){window.webkit.messageHandlers.external.postMessage(m);}}};}";
        WKUserScript *bs = [[WKUserScript alloc] initWithSource:bridge injectionTime:WKUserScriptInjectionTimeAtDocumentStart forMainFrameOnly:YES];
        [ucc addUserScript:bs];
        WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
        cfg.userContentController = ucc;
        sidebarWebView = [[WKWebView alloc] initWithFrame:NSMakeRect(0,0,52,700) configuration:cfg];
        sidebarWebView.autoresizingMask = NSViewHeightSizable;
        sidebarWebView.hidden = YES;
        [[playerWindow contentView] addSubview:sidebarWebView];
    });
}

void loadSidebarHTML(const char* html) {
    NSString *nsHTML = html ? [NSString stringWithUTF8String:html] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        if (sidebarWebView) [sidebarWebView loadHTMLString:nsHTML baseURL:nil];
    });
}

void evalSidebarJS(const char* js) {
    NSString *nsJS = js ? [NSString stringWithUTF8String:js] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        if (sidebarWebView) [sidebarWebView evaluateJavaScript:nsJS completionHandler:nil];
    });
}

void showSidebarView(int expanded) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!sidebarWebView) return;
        sidebarWebView.hidden = NO;
        [sidebarWebView setFrameSize:NSMakeSize(expanded?300:52, sidebarWebView.bounds.size.height)];
    });
}
void hideSidebarView(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ if(sidebarWebView) sidebarWebView.hidden = YES; });
}

void showConnOverlay(const char* text, int clickable) {
    NSString *nsText = text ? [NSString stringWithUTF8String:text] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        if (connOverlay) { [connOverlay removeFromSuperview]; connOverlay = nil; }
        NSRect mf = metalView ? metalView.bounds : NSMakeRect(0,0,400,300);
        CGFloat ow = 380, oh = 56;
        NSRect of = NSMakeRect((mf.size.width-ow)/2, (mf.size.height-oh)/2, ow, oh);
        if (clickable) {
            ClickableOverlay *cv = [[ClickableOverlay alloc] initWithFrame:of];
            cv.clickTag = 1; cv.wantsLayer = YES;
            cv.layer.backgroundColor = [NSColor colorWithCalibratedWhite:1.0 alpha:0.92].CGColor;
            cv.layer.cornerRadius = 10;
            // Accent top line
            NSView *accent = [[NSView alloc] initWithFrame:NSMakeRect(0,oh-3,ow,3)];
            accent.wantsLayer=YES; accent.layer.backgroundColor=[NSColor colorWithRed:0.2 green:0.5 blue:0.9 alpha:1.0].CGColor;
            [cv addSubview:accent];
            NSTextField *lb = [NSTextField labelWithString:nsText];
            [lb setAlignment:NSTextAlignmentCenter]; [lb setFont:[NSFont systemFontOfSize:14]];
            [lb setTextColor:[NSColor colorWithCalibratedWhite:0.3 alpha:1.0]];
            [lb setFrame:NSMakeRect(8,4,ow-16,oh-12)];
            [cv addSubview:lb]; connOverlay = cv;
        } else {
            NSView *cv = [[NSView alloc] initWithFrame:of];
            cv.wantsLayer = YES;
            cv.layer.backgroundColor = [NSColor colorWithCalibratedWhite:1.0 alpha:0.92].CGColor;
            cv.layer.cornerRadius = 10;
            // Accent top line
            NSView *accent = [[NSView alloc] initWithFrame:NSMakeRect(0,oh-3,ow,3)];
            accent.wantsLayer=YES; accent.layer.backgroundColor=[NSColor colorWithRed:0.2 green:0.5 blue:0.9 alpha:1.0].CGColor;
            [cv addSubview:accent];
            NSTextField *lb = [NSTextField labelWithString:nsText];
            [lb setAlignment:NSTextAlignmentCenter]; [lb setFont:[NSFont systemFontOfSize:14]];
            [lb setTextColor:[NSColor colorWithCalibratedWhite:0.3 alpha:1.0]];
            [lb setFrame:NSMakeRect(8,4,ow-16,oh-12)];
            [cv addSubview:lb]; connOverlay = cv;
        }
        [metalView addSubview:connOverlay];
    });
}

void hideConnOverlay(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ if(connOverlay){[connOverlay removeFromSuperview];connOverlay=nil;} });
}

void showUploadOvl(const char* name, const char* detail, int pct) {
    NSString *nsName = name ? [NSString stringWithUTF8String:name] : @"";
    NSString *nsDetail = detail ? [NSString stringWithUTF8String:detail] : @"";
    dispatch_async(dispatch_get_main_queue(), ^{
        if (uploadOverlay) { [uploadOverlay removeFromSuperview]; uploadOverlay = nil; }
        NSRect mf = metalView ? metalView.bounds : NSMakeRect(0,0,400,300);
        CGFloat ow = 300, oh = 70;
        NSRect of = NSMakeRect((mf.size.width-ow)/2, (mf.size.height-oh)/2, ow, oh);
        NSView *uv = [[NSView alloc] initWithFrame:of];
        uv.wantsLayer = YES;
        uv.layer.backgroundColor = [NSColor colorWithCalibratedWhite:1.0 alpha:0.95].CGColor;
        uv.layer.cornerRadius = 8;
        NSString *txt = [NSString stringWithFormat:@" %@  %d%%\n %@", nsName, pct, nsDetail];
        NSTextField *lb = [NSTextField labelWithString:txt];
        [lb setFont:[NSFont systemFontOfSize:12]];
        [lb setTextColor:[NSColor colorWithCalibratedWhite:0.3 alpha:1.0]];
        [lb setFrame:NSMakeRect(8,16,ow-16,oh-24)];
        [uv addSubview:lb];
        // Progress bar
        if (pct > 0) {
            CGFloat barH = 4, barY = oh - barH - 6;
            NSView *barBg = [[NSView alloc] initWithFrame:NSMakeRect(8,barY,ow-16,barH)];
            barBg.wantsLayer=YES; barBg.layer.backgroundColor=[NSColor colorWithCalibratedWhite:0.9 alpha:1.0].CGColor;
            barBg.layer.cornerRadius = 2;
            [uv addSubview:barBg];
            CGFloat fillW = (ow-16) * pct / 100.0;
            NSView *barFill = [[NSView alloc] initWithFrame:NSMakeRect(8,barY,fillW,barH)];
            barFill.wantsLayer=YES; barFill.layer.backgroundColor=[NSColor colorWithRed:0.2 green:0.5 blue:0.9 alpha:1.0].CGColor;
            barFill.layer.cornerRadius = 2;
            [uv addSubview:barFill];
        }
        uploadOverlay = uv;
        [metalView addSubview:uploadOverlay];
    });
}

void hideUploadOvl(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ if(uploadOverlay){[uploadOverlay removeFromSuperview];uploadOverlay=nil;} });
}

void showTOvl(const char* line1, const char* line2, const char* action) {
    NSString *nsL1 = line1 ? [NSString stringWithUTF8String:line1] : @"";
    NSString *nsL2 = line2 ? [NSString stringWithUTF8String:line2] : @"";
    NSString *nsAction = (action && action[0]) ? [NSString stringWithUTF8String:action] : nil;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (toastOverlay) { [toastOverlay removeFromSuperview]; toastOverlay = nil; }
        NSRect mf = metalView ? metalView.bounds : NSMakeRect(0,0,400,300);
        CGFloat ow = 340, oh = 80;
        NSRect of = NSMakeRect((mf.size.width-ow)/2, mf.size.height/2-oh/2-60, ow, oh);
        ClickableOverlay *tv = [[ClickableOverlay alloc] initWithFrame:of];
        tv.clickTag = 2; tv.wantsLayer = YES;
        tv.layer.backgroundColor = [NSColor colorWithCalibratedWhite:1.0 alpha:0.97].CGColor;
        tv.layer.cornerRadius = 8;
        tv.layer.borderColor = [NSColor colorWithCalibratedWhite:0.88 alpha:1.0].CGColor;
        tv.layer.borderWidth = 1.0;
        // Accent left bar
        NSView *accent = [[NSView alloc] initWithFrame:NSMakeRect(0,4,4,oh-8)];
        accent.wantsLayer=YES; accent.layer.backgroundColor=[NSColor colorWithRed:0.85 green:0.56 blue:0.29 alpha:1.0].CGColor;
        accent.layer.cornerRadius = 2;
        [tv addSubview:accent];
        NSString *txt = [NSString stringWithFormat:@"%@\n%@", nsL1, nsL2];
        NSTextField *lb = [NSTextField labelWithString:txt];
        [lb setFont:[NSFont systemFontOfSize:12]];
        [lb setTextColor:[NSColor colorWithCalibratedWhite:0.3 alpha:1.0]];
        [lb setFrame:NSMakeRect(14,10,ow-130,oh-20)];
        [tv addSubview:lb];
        if (nsAction) {
            // Visual button only — the entire toast overlay handles clicks via EV_OVERLAY_CLICK
            NSView *btnBg = [[NSView alloc] initWithFrame:NSMakeRect(ow-100,20,80,36)];
            btnBg.wantsLayer = YES; btnBg.layer.backgroundColor = [NSColor colorWithRed:0.85 green:0.56 blue:0.29 alpha:1.0].CGColor;
            btnBg.layer.cornerRadius = 6;
            NSTextField *btnLb = [NSTextField labelWithString:nsAction];
            [btnLb setFont:[NSFont boldSystemFontOfSize:12]]; [btnLb setTextColor:[NSColor whiteColor]];
            [btnLb setAlignment:NSTextAlignmentCenter]; [btnLb setFrame:NSMakeRect(0,8,80,20)];
            [btnBg addSubview:btnLb]; [tv addSubview:btnBg];
        }
        toastOverlay = tv;
        [metalView addSubview:toastOverlay];
    });
}

void hideTOvl(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ if(toastOverlay){[toastOverlay removeFromSuperview];toastOverlay=nil;} });
}

void toggleFullscreen(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ [playerWindow toggleFullScreen:nil]; });
}

void setMinWindowSize(int w, int h) {
    dispatch_async(dispatch_get_main_queue(), ^{ [playerWindow setMinSize:NSMakeSize(w,h)]; });
}

void setTopmost(int on) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [playerWindow setLevel:on ? NSFloatingWindowLevel : NSNormalWindowLevel];
    });
}

void setAspectRatio(float r) {
    dispatch_async(dispatch_get_main_queue(), ^{
        AppDelegate *del = (AppDelegate *)[NSApp delegate];
        del.aspectRatio = r;
        [playerWindow setContentAspectRatio:NSMakeSize(r, 1.0)];
    });
}

void resizeWindow(int w, int h) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSRect fr = [playerWindow frame];
        NSRect cr = [playerWindow contentRectForFrameRect:fr];
        CGFloat dw = w - cr.size.width, dh = h - cr.size.height;
        [playerWindow setFrame:NSMakeRect(fr.origin.x, fr.origin.y - dh, fr.size.width + dw, fr.size.height + dh) display:YES];
    });
}

void setWindowPosition(int x, int y) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!playerWindow) return;
        NSRect fr = [playerWindow frame];
        [playerWindow setFrameOrigin:NSMakePoint(x, y)];
    });
}

void setTitle(const char* t) {
    if (!t || !t[0]) return;
    NSString *nsTitle = [NSString stringWithUTF8String:t];
    dispatch_async(dispatch_get_main_queue(), ^{ if(playerWindow && nsTitle)[playerWindow setTitle:nsTitle]; });
}

void stopApp(void) {
    dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; });
}

void runAppLoop(void) { [NSApp run]; }
*/
import "C"

import (
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func init() {
	_newPlatformUIImpl = func(app *App) PlatformUI {
		return NewDarwinUI(app)
	}
}

type DarwinUI struct {
	app        *App
	mu         sync.Mutex
	expanded   bool
	fullscreen bool
	baseW      int
	baseH      int
	sideW      int

	wheelDir    int
	wheelCount  int
	wheelLast   time.Time
	wheelTimer  *time.Timer
}

func NewDarwinUI(app *App) *DarwinUI {
	ui := &DarwinUI{app: app}
	if app.Controlled {
		ui.baseW, ui.baseH = 464, 780
		if matrixScale > 0 && matrixScale < 1.0 {
			ui.baseW = int(float64(ui.baseW) * matrixScale)
			ui.baseH = int(float64(ui.baseH) * matrixScale)
		}
		ui.sideW = darwinSideCollapsedW
	} else if app.Landscape {
		ui.baseW, ui.baseH = 900+darwinSideCollapsedW, 600
		ui.sideW = darwinSideCollapsedW
	} else {
		ui.baseW, ui.baseH = 412+darwinSideCollapsedW, 780
		ui.sideW = darwinSideCollapsedW
	}
	return ui
}

func (ui *DarwinUI) cString(s string) *C.char {
	return C.CString(s)
}

// ─── Run ────────────────────────────────────────────────────────────────

func (ui *DarwinUI) Run() {
	app := ui.app
	runtime.LockOSThread()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; C.stopApp() }()
	app.OnSignalShutdown = func() { C.stopApp() }

	C.createPlayerWindow()
	C.createSidebar()

	// 启动本地 UDP 排列监听
	startArrangeListener(func() {
		ui.doArrangeWindowsFromBroadcast()
	}, func(addr string) {
		ui.app.SetPrimaryAddr(addr)
		ui.syncSidebarState()
	})

	viewPtr := uintptr(C.getMetalView())
	if viewPtr != 0 {
		rotated := app.Rotated.Load()
		var initW, initH int32 = 412, 732
		if app.Controlled {
			initW, initH = int32(ui.baseW), int32(ui.baseH)
		} else if app.Landscape {
			initW, initH = 900, 600
		}
		ren, err := NewMetalRenderer(viewPtr, int(initW), int(initH), rotated, *flagFill)
		if err != nil {
			logf("[mac] MetalRenderer create failed: %v\n", err)
		} else {
			app.MetalRen.Store(ren)
			ren.RenderIdle()
			logf("[mac] MetalRenderer created\n")
		}
	}

	app.OnFirstFrame = func() { go ui.onFirstFrame() }
	app.ClipboardWriter = func(text string) {
		darwinWriteClipboard(text)
	}
	app.OnConnStateChange = func(state, retry int) {
		ui.updateConnOverlay(state, retry)
		ui.syncSidebarState()
	}
	app.OnScreenshot = func() { ui.onScreenshot() }

	html := composeSidePanelHTML()
	if list := ctrlListJSON(app.CtrlItems); list != "[]" {
		html = strings.Replace(html, "ctrlList:[]", "ctrlList:"+list, 1)
	}
	C.loadSidebarHTML(ui.cString(html))
	ui.syncSidebarState()

	if *flagTitle != "" {
		cs := C.CString(*flagTitle)
		C.setTitle(cs)
		C.free(unsafe.Pointer(cs))
	}

	cShowSidebarView(0)
	C.layoutViews(C.int(ui.baseW), C.int(ui.baseH), C.int(ui.sideW), C.int(darwinNavBarH))

	go ui.renderLoop()

	if app.RTSPURL != "" || app.CastMode == CastModeTebox {
		go func() {
			if err := app.Connect(); err != nil {
				logf("[mac] connect failed: %v\n", err)
			}
		}()
	}

	C.runAppLoop()
	app.Disconnect()
}

// ─── Render loop ────────────────────────────────────────────────────────

func (ui *DarwinUI) processEvents() {
	var events [64]C.DarwinEvent
	n := int(C.drainEvents(&events[0], 64))
	for i := 0; i < n; i++ {
		ev := events[i]
		switch ev.evType {
		case C.EV_MOUSE_DOWN:
			ui.handleMouseClick(true, int(ev.button), int(ev.x), int(ev.y))
		case C.EV_MOUSE_UP:
			ui.handleMouseClick(false, int(ev.button), int(ev.x), int(ev.y))
		case C.EV_DBL_CLICK:
			ui.handleDoubleClick(int(ev.x), int(ev.y))
		case C.EV_MOUSE_MOVE:
			ui.handleMouseMove(int(ev.x), int(ev.y), int(ev.buttonState))
		case C.EV_MOUSE_LEAVE:
			ui.handleMouseLeave()
		case C.EV_KEY_DOWN:
			ui.handleKey(true, int(ev.keycode), int(ev.mods), uint32(ev.unicode), uint32(ev.textChar))
		case C.EV_KEY_UP:
			ui.handleKey(false, int(ev.keycode), int(ev.mods), uint32(ev.unicode), uint32(ev.textChar))
		case C.EV_SCROLL:
			ui.handleScroll(int(ev.x), int(ev.y), float32(ev.scrollDY))
		case C.EV_RESIZE:
			ui.handleResize(int(ev.width), int(ev.height))
		case C.EV_WIN_RESIZE:
			ui.mu.Lock()
			ui.baseW = int(ev.width)
			ui.baseH = int(ev.height)
			ui.mu.Unlock()
			ui.layoutChildren()
		case C.EV_NAV_BACK:
			ui.app.SendCmd(CmdAndroidKey, KeyBack)
		case C.EV_NAV_HOME:
			ui.app.SendCmd(CmdAndroidKey, KeyHome)
		case C.EV_NAV_RECENT:
			ui.app.SendCmd(CmdAndroidKey, KeyRecent)
		case C.EV_OVERLAY_CLICK:
			if ev.intArg == 1 {
				go ui.app.ManualReconnect()
			} else if ev.intArg == 2 {
				ui.app.screenshotMu.Lock()
				path := ui.app.screenshotPath
				ui.app.screenshotMu.Unlock()
				if path != "" {
					exec.Command("open", "-R", path).Run()
				}
				cHideToast()
			}
		}
	}
}

func (ui *DarwinUI) processSidebarMessages() {
	var buf [8192]byte
	n := int(C.getSidebarMsg((*C.char)(unsafe.Pointer(&buf[0])), 8192))
	if n > 0 {
		ui.app.dispatchMessage(string(buf[:n]), ui)
	}
}

func (ui *DarwinUI) processDropPaths() {
	var buf [2048]byte
	n := int(C.getDropPath((*C.char)(unsafe.Pointer(&buf[0])), 2048))
	if n > 0 {
		darwinStartFileUpload(ui.app, string(buf[:n]))
	}
}

func cLayoutViews(w, h, sideW, navH int) {
	C.layoutViews(C.int(w), C.int(h), C.int(sideW), C.int(navH))
}

func cEvalSidebarJS(js string) {
	cs := C.CString(js)
	defer C.free(unsafe.Pointer(cs))
	C.evalSidebarJS(cs)
}

func cShowSidebarView(expanded int) { C.showSidebarView(C.int(expanded)) }
func cHideSidebarView()             { C.hideSidebarView() }

func cShowConnOverlay(text string, clickable bool) {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	click := C.int(0)
	if clickable {
		click = 1
	}
	C.showConnOverlay(cs, click)
}

func cHideConnOverlay() { C.hideConnOverlay() }

func cShowUploadOverlay(name, detail string, pct int) {
	cn := C.CString(name)
	cd := C.CString(detail)
	defer C.free(unsafe.Pointer(cn))
	defer C.free(unsafe.Pointer(cd))
	C.showUploadOvl(cn, cd, C.int(pct))
}

func cHideUploadOverlay() { C.hideUploadOvl() }

func cShowToast(line1, line2, action string) {
	c1 := C.CString(line1)
	c2 := C.CString(line2)
	c3 := C.CString(action)
	defer C.free(unsafe.Pointer(c1))
	defer C.free(unsafe.Pointer(c2))
	defer C.free(unsafe.Pointer(c3))
	C.showTOvl(c1, c2, c3)
}

func cHideToast() { C.hideTOvl() }

func cToggleFullscreen() { C.toggleFullscreen() }

func cSetTopmost(on bool) {
	if on {
		C.setTopmost(1)
	} else {
		C.setTopmost(0)
	}
}

func cSetAspectRatio(ratio float32) { C.setAspectRatio(C.float(ratio)) }
func cResizeWindow(w, h int)        { C.resizeWindow(C.int(w), C.int(h)) }
func cSetWindowPosition(x, y int)   { C.setWindowPosition(C.int(x), C.int(y)) }
func cSetMinWindowSize(w, h int)    { C.setMinWindowSize(C.int(w), C.int(h)) }

