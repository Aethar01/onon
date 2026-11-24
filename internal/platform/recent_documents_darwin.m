#import <Cocoa/Cocoa.h>

void ononNoteRecentDocument(const char *path) {
    NSURL *url = [[NSURL alloc] initFileURLWithPath:[NSString stringWithUTF8String:path]];
    dispatch_async(dispatch_get_main_queue(), ^{
        [[NSDocumentController sharedDocumentController] noteNewRecentDocumentURL:url];
        [url release];
    });
}
