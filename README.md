# Warsow log parser

./wsw_server.x86_64 | ./warsowlog -p ./path/to/file.log

If you have issue with pipe buffering:

stdbuf -oL -eL ./wsw_server.x86_64 | ./warsowlog -p ./path/to/file.log

