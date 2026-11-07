#!/usr/bin/env python3
# Launcher for the XPAM browser service. The service logic lives in the server module, which ships
# compiled to a native extension (server*.so) with no readable source; this thin entry point simply
# starts it. (When the build is run with XPAM_COMPILE=0, server.py is still importable the same way.)
from server import main

if __name__ == '__main__':
    main()
