//go:build darwin
#import <Cocoa/Cocoa.h>
extern void freeturn_tray_action(int action);
@interface FreeTurnTrayTarget : NSObject
- (void)show:(id)sender;
- (void)quit:(id)sender;
@end
@implementation FreeTurnTrayTarget
- (void)show:(id)sender { freeturn_tray_action(1); }
- (void)quit:(id)sender { freeturn_tray_action(2); }
@end
static NSStatusItem *statusItem;
static FreeTurnTrayTarget *target;
static id launchObserver;
// Wails routes the native Quit role through OnBeforeClose. When close-to-tray
// is enabled that would hide the app instead of quitting. Route Cmd+Q through
// the same explicit Quit callback as the status-menu action.
static void bindQuitShortcut(void) {
 for (NSMenuItem *root in NSApp.mainMenu.itemArray) {
  for (NSMenuItem *item in root.submenu.itemArray) {
   if ([item.keyEquivalent.lowercaseString isEqualToString:@"q"] && (item.keyEquivalentModifierMask & NSEventModifierFlagCommand)) {
    item.target=target; item.action=@selector(quit:);
   }
  }
 }
}
void freeturn_tray_start(const char *title, const char *open, const char *quit) {
 NSString *name=[NSString stringWithUTF8String:title],*openTitle=[NSString stringWithUTF8String:open],*quitTitle=[NSString stringWithUTF8String:quit];
 dispatch_async(dispatch_get_main_queue(), ^{
  target=[FreeTurnTrayTarget new];
  launchObserver=[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidFinishLaunchingNotification object:nil queue:[NSOperationQueue mainQueue] usingBlock:^(NSNotification *note) { bindQuitShortcut(); }];
  bindQuitShortcut();
  statusItem=[[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
  statusItem.button.title=@"FT";
  statusItem.button.toolTip=name;
  NSMenu *menu=[NSMenu new];
  NSMenuItem *show=[[NSMenuItem alloc] initWithTitle:openTitle action:@selector(show:) keyEquivalent:@""]; show.target=target;
  NSMenuItem *exit=[[NSMenuItem alloc] initWithTitle:quitTitle action:@selector(quit:) keyEquivalent:@""]; exit.target=target;
  [menu addItem:show]; [menu addItem:[NSMenuItem separatorItem]]; [menu addItem:exit]; statusItem.menu=menu;
 });
}
void freeturn_tray_close(void) {
 dispatch_async(dispatch_get_main_queue(), ^{ if(launchObserver) [[NSNotificationCenter defaultCenter] removeObserver:launchObserver]; launchObserver=nil; if(statusItem) [[NSStatusBar systemStatusBar] removeStatusItem:statusItem]; statusItem=nil; target=nil; });
}
void freeturn_tray_language(const char *open, const char *quit) {
 NSString *a=[NSString stringWithUTF8String:open],*b=[NSString stringWithUTF8String:quit];
 dispatch_async(dispatch_get_main_queue(), ^{ if(statusItem) { [statusItem.menu itemAtIndex:0].title=a; [statusItem.menu itemAtIndex:2].title=b; } });
}
