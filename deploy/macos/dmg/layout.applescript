-- Lays out the window of MyInfra.dmg in the Finder, which then writes the
-- .DS_Store it reads back: the app on the left, Applications on the right,
-- over background.html. deploy/macos/package.sh runs it on the mounted image.
--   osascript layout.applescript VOLUME
on run argv
	set volumeName to item 1 of argv
	tell application "Finder"
		tell disk volumeName
			open
			set current view of container window to icon view
			set toolbar visible of container window to false
			set statusbar visible of container window to false
			try
				set pathbar visible of container window to false
			end try
			-- 660 × 400 inside, under the title bar.
			set the bounds of container window to {200, 120, 860, 548}
			set viewOptions to the icon view options of container window
			set arrangement of viewOptions to not arranged
			set icon size of viewOptions to 112
			set text size of viewOptions to 13
			set background picture of viewOptions to file ".background:background.tiff"
			set position of item "MyInfra.app" of container window to {170, 190}
			set position of item "Applications" of container window to {490, 190}
			-- Out of sight for those who show hidden files.
			repeat with hiddenName in {".background", ".VolumeIcon.icns", ".fseventsd", ".Trashes", ".DS_Store"}
				try
					set position of item (hiddenName as text) of container window to {900, 900}
				end try
			end repeat
			close
			open
			update without registering applications
			delay 2
			close
		end tell
	end tell
end run
