//go:build darwin

#import <Cocoa/Cocoa.h>

extern void goTrayCallback(int actionID);

static const void *globalTrayIconBytes = NULL;
static int globalTrayIconLen = 0;

@interface PDAppDelegate : NSObject <NSApplicationDelegate>
@property (strong, nonatomic) NSStatusItem *statusItem;
@property (strong, nonatomic) NSMenuItem *statusMenuItem;
@property (strong, nonatomic) NSMenuItem *pwdMenuItem;
@property (strong, nonatomic) NSMenuItem *toggleShareItem;
@property (strong, nonatomic) NSMenuItem *toggleEditItem;
@end

@implementation PDAppDelegate

- (void)applicationDidFinishLaunching:(NSNotification *)aNotification {
    self.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    self.statusItem.button.title = @"";
    if (globalTrayIconLen > 0 && globalTrayIconBytes != NULL) {
        NSData *pngData = [NSData dataWithBytes:globalTrayIconBytes length:globalTrayIconLen];
        NSImage *img = [[NSImage alloc] initWithData:pngData];
        if (img != nil) {
            [img setSize:NSMakeSize(18, 18)];
            self.statusItem.button.image = img;
            self.statusItem.button.imagePosition = NSImageOnly;
        }
    } else {
        self.statusItem.button.title = @"TD";
    }

    NSMenu *menu = [[NSMenu alloc] initWithTitle:@"TinyDrop"];

    NSMenuItem *openPanelItem = [[NSMenuItem alloc] initWithTitle:@"🖥️ Open TinyDrop Dashboard" action:@selector(onMenuClick:) keyEquivalent:@"o"];
    openPanelItem.tag = 1;
    openPanelItem.target = self;
    [menu addItem:openPanelItem];

    NSMenuItem *openTempItem = [[NSMenuItem alloc] initWithTitle:@"📂 Open Temp Share Folder" action:@selector(onMenuClick:) keyEquivalent:@"d"];
    openTempItem.tag = 2;
    openTempItem.target = self;
    [menu addItem:openTempItem];

    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *addFileItem = [[NSMenuItem alloc] initWithTitle:@"➕ Add Files to Share..." action:@selector(onMenuClick:) keyEquivalent:@"f"];
    addFileItem.tag = 3;
    addFileItem.target = self;
    [menu addItem:addFileItem];

    NSMenuItem *addDirItem = [[NSMenuItem alloc] initWithTitle:@"📁 Add Folders to Share..." action:@selector(onMenuClick:) keyEquivalent:@""];
    addDirItem.tag = 4;
    addDirItem.target = self;
    [menu addItem:addDirItem];

    [menu addItem:[NSMenuItem separatorItem]];

    self.toggleShareItem = [[NSMenuItem alloc] initWithTitle:@"✅ Toggle File Sharing On/Off" action:@selector(onMenuClick:) keyEquivalent:@""];
    self.toggleShareItem.tag = 5;
    self.toggleShareItem.target = self;
    [menu addItem:self.toggleShareItem];

    self.toggleEditItem = [[NSMenuItem alloc] initWithTitle:@"🔒 Toggle Client Read/Write Permission" action:@selector(onMenuClick:) keyEquivalent:@""];
    self.toggleEditItem.tag = 6;
    self.toggleEditItem.target = self;
    [menu addItem:self.toggleEditItem];

    self.pwdMenuItem = [[NSMenuItem alloc] initWithTitle:@"🔑 Regenerate 6-Digit PIN Code" action:@selector(onMenuClick:) keyEquivalent:@""];
    self.pwdMenuItem.tag = 7;
    self.pwdMenuItem.target = self;
    [menu addItem:self.pwdMenuItem];

    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"Quit TinyDrop" action:@selector(onMenuClick:) keyEquivalent:@"q"];
    quitItem.tag = 99;
    quitItem.target = self;
    [menu addItem:quitItem];

    self.statusItem.menu = menu;
}

- (void)onMenuClick:(id)sender {
    NSMenuItem *item = (NSMenuItem *)sender;
    goTrayCallback((int)item.tag);
}

@end

static PDAppDelegate *globalDelegate = nil;

void runMacTray(const void *iconBytes, int iconLen) {
    globalTrayIconBytes = iconBytes;
    globalTrayIconLen = iconLen;
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        if (iconLen > 0 && iconBytes != NULL) {
            NSData *pngData = [NSData dataWithBytes:iconBytes length:iconLen];
            NSImage *appIcon = [[NSImage alloc] initWithData:pngData];
            if (appIcon != nil) {
                [NSApp setApplicationIconImage:appIcon];
            }
        }
        globalDelegate = [[PDAppDelegate alloc] init];
        [NSApp setDelegate:globalDelegate];
        [NSApp run];
    }
}

void stopMacTray(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp terminate:nil];
    });
}
