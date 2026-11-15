### Fixed
- A WebDAV save over an existing file replaces it. The upload goes to a unique temporary resource beside the target and is published with a standard MOVE that overwrites, so a server that stores a colliding PUT under another name no longer leaves the old contents at the target (#71).
- A failed WebDAV save, at PUT, part-way through PUT, or at MOVE, keeps the original contents, reports the error and removes its temporary upload (#68).
- The unified and WebDAV C interfaces report a failed upload or a short write instead of returning success before checking the upload result (#71).
