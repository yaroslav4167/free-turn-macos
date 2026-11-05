//go:build darwin
#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>
extern void freeturn_notification_action(unsigned long long callback);
@interface FreeTurnNotificationDelegate : NSObject<UNUserNotificationCenterDelegate>
@end
@implementation FreeTurnNotificationDelegate
- (void)userNotificationCenter:(UNUserNotificationCenter *)center willPresentNotification:(UNNotification *)notification withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completion {
 completion(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionSound);
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center didReceiveNotificationResponse:(UNNotificationResponse *)response withCompletionHandler:(void (^)(void))completion {
 if ([response.actionIdentifier isEqualToString:UNNotificationDefaultActionIdentifier]) {
  freeturn_notification_action([response.notification.request.content.userInfo[@"callback"] unsignedLongLongValue]);
 }
 completion();
}
@end
static FreeTurnNotificationDelegate *notificationDelegate;
void freeturn_notify(const char *title, const char *body, unsigned long long callback) {
 NSString *a=[NSString stringWithUTF8String:title],*b=[NSString stringWithUTF8String:body];
 dispatch_async(dispatch_get_main_queue(), ^{
  // Unit tests and unbundled CLI processes must not ask for notification access.
  if (![NSBundle mainBundle].bundleIdentifier) return;
  UNUserNotificationCenter *center=[UNUserNotificationCenter currentNotificationCenter];
  if(!notificationDelegate) notificationDelegate=[FreeTurnNotificationDelegate new]; center.delegate=notificationDelegate;
  [center requestAuthorizationWithOptions:UNAuthorizationOptionAlert|UNAuthorizationOptionSound completionHandler:^(BOOL granted,NSError *error) {
   if(!granted || error) return;
   UNMutableNotificationContent *content=[UNMutableNotificationContent new]; content.title=a; content.body=b;
   content.sound=[UNNotificationSound defaultSound]; content.userInfo=@{@"callback":@(callback)};
   UNNotificationRequest *request=[UNNotificationRequest requestWithIdentifier:@"freeturn-vk-captcha" content:content trigger:nil];
   [center addNotificationRequest:request withCompletionHandler:^(NSError *error) { if(error) NSLog(@"FreeTurn notification: %@",error.localizedDescription); }];
  }];
 });
}
