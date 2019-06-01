simple bittorrent client

created as demo app for duit


# todo

- after latest torrent update, setting max rate causes crash, find cause
- when adding torrent, begin downloading immediately. currently needs a click on start.
- fix bug where torrent details are being cleared all the time.
- store state, and restore state on open
- allow setting per file whether you want to download it, and show progress per file
- allow start/pause for selection of multiple torrents
- show where files are saved, let user change location?
- show current overal status:
	- peers, dht status, total download/upload rate, total download/upload size
- deal with .torrent files too
