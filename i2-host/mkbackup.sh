#!/bin/sh
cp -a sqlite.db "backup/`date --rfc-3339=seconds`.db"
