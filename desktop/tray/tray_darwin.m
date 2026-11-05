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
void freeturn_tray_start(const char *title, const char *open, const char *quit) {
 NSString *name=[NSString stringWithUTF8String:title],*openTitle=[NSString stringWithUTF8String:open],*quitTitle=[NSString stringWithUTF8String:quit];
 dispatch_async(dispatch_get_main_queue(), ^{
  target=[FreeTurnTrayTarget new];
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
 dispatch_async(dispatch_get_main_queue(), ^{ if(statusItem) [[NSStatusBar systemStatusBar] removeStatusItem:statusItem]; statusItem=nil; target=nil; });
}
void freeturn_tray_language(const char *open, const char *quit) {
 NSString *a=[NSString stringWithUTF8String:open],*b=[NSString stringWithUTF8String:quit];
 dispatch_async(dispatch_get_main_queue(), ^{ if(statusItem) { [statusItem.menu itemAtIndex:0].title=a; [statusItem.menu itemAtIndex:2].title=b; } });
}
