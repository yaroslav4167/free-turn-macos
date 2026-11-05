//go:build darwin
#import <Foundation/Foundation.h>
#import <SystemConfiguration/SystemConfiguration.h>
#include <stdint.h>
uintptr_t freeturn_dns_create(const char *name, const char *servers) {
 @autoreleasepool {
  NSDictionary *options=@{(__bridge NSString *)kSCDynamicStoreUseSessionKeys:@YES};
  SCDynamicStoreRef store=SCDynamicStoreCreateWithOptions(NULL,CFSTR("FreeTurn VPN"),(__bridge CFDictionaryRef)options,NULL,NULL);
  if (!store) return 0;
  NSString *iface=[NSString stringWithUTF8String:name];
  NSString *key=[NSString stringWithFormat:@"State:/Network/Service/org.freeturn.%@/DNS",iface];
  NSDictionary *dns=@{@"ServerAddresses":[[NSString stringWithUTF8String:servers] componentsSeparatedByString:@","],@"SupplementalMatchDomains":@[@""],@"SupplementalMatchOrders":@[@0],@"InterfaceName":iface};
  if (!SCDynamicStoreSetValue(store,(__bridge CFStringRef)key,(__bridge CFDictionaryRef)dns)) { CFRelease(store); return 0; }
  return (uintptr_t)store;
 }
}
// Session-owned DNS keys are removed by configd even if the helper is killed.
void freeturn_dns_release(uintptr_t store) { if (store) CFRelease((SCDynamicStoreRef)store); }
