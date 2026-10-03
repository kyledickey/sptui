# Installs sptui from its latest GitHub release, on Windows.
#
#   irm https://sptui.sh/install.ps1 | iex
#
# Environment:
#   SPTUI_VERSION      release to install, like v1.2.0 (default: the latest)
#   SPTUI_INSTALL_DIR  where to put sptui.exe (default: %LOCALAPPDATA%\Programs\sptui)
#   SPTUI_ARCHIVE      install this sptui_windows_<arch>.zip instead of downloading one
#
# Runs in Windows PowerShell 5.1 and PowerShell 7, on x86-64 and ARM (which
# runs the x86-64 build). It puts the install directory on your PATH.
#
# The file is plain ASCII, so Windows PowerShell reads it the same however
# it gets it; the few other characters it prints are made from their codes.

# Everything is in one script block, so piping it into iex leaves nothing
# behind in your session, and a failure ends the install, not your window.
& {
	$ErrorActionPreference = 'Stop'
	# Windows PowerShell's progress bar slows downloads to a crawl.
	$ProgressPreference = 'SilentlyContinue'
	Set-StrictMode -Version 3

	$Repo = 'kyledickey/sptui'
	$Version = if ($env:SPTUI_VERSION) { $env:SPTUI_VERSION } else { 'latest' }
	$InstallDir = $env:SPTUI_INSTALL_DIR # set in main, once we know this is Windows
	$Archive = $env:SPTUI_ARCHIVE

	$Check = [char]0x2713
	$Cross = [char]0x2717
	$Bar = [char]0x258C
	$Dot = [char]0x00B7
	$Ellipsis = [char]0x2026

	# --- output ----------------------------------------------------------

	function banner {
		# The logo, with _ # ^ for the lower, full and upper blocks.
		$art = '_#^ #^_ ^#^ # # ^#^', '__^ #^   #  #_# _#_'
		Write-Host
		foreach ($line in $art) {
			$line = $line.Replace('_', [char]0x2584).Replace('#', [char]0x2588).Replace('^', [char]0x2580)
			Write-Host "  $line" -ForegroundColor Green
		}
		Write-Host "  spotify in your terminal $Dot installer" -ForegroundColor DarkGray
		Write-Host
	}

	# step prints a section heading.
	function step([string]$Text) { Write-Host "$Bar $Text" -ForegroundColor Green }

	function ok([string]$Text) {
		Write-Host "  $Check " -ForegroundColor Green -NoNewline
		Write-Host $Text
	}

	function note([string]$Text) { Write-Host "  $Text" -ForegroundColor DarkGray }

	function die([string]$Text, [string[]]$Detail = @()) {
		Write-Host
		Write-Host "$Cross $Text" -ForegroundColor Red
		foreach ($d in $Detail) { Write-Host "  $d" -ForegroundColor DarkGray }
		Write-Host
		throw 'sptui-install-failed'
	}

	# --- install ---------------------------------------------------------

	function detect_arch {
		# A 32-bit PowerShell on 64-bit Windows says x86 here, and the truth
		# in PROCESSOR_ARCHITEW6432.
		$arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
		switch ($arch) {
			'AMD64' { return 'amd64' }
			'ARM64' { return 'amd64' } # Windows 11 runs x86-64 programs on ARM
			default { die "No sptui build for $arch" "Build it yourself: https://github.com/$Repo#install" }
		}
	}

	function fetch([string]$Url, [string]$Path) {
		try {
			Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $Path
		} catch {
			die "Couldn't download $Url" $_.Exception.Message
		}
	}

	# verify checks the archive against the release's checksums.txt.
	function verify([string]$Sums, [string]$File) {
		$want = $null
		foreach ($line in Get-Content -LiteralPath $Sums) {
			$sum, $name = $line -split '\s+', 2
			if ($name -and $name.TrimStart('*') -eq $File) { $want = $sum }
		}
		if (-not $want) { die "$File isn't in checksums.txt" }
		$got = (Get-FileHash -LiteralPath (Join-Path $tmp $File) -Algorithm SHA256).Hash
		if ($got -ne $want) { die 'Checksum mismatch' "got $got", "want $want" }
	}

	function install_sptui([string]$Arch) {
		step 'sptui'
		$file = "sptui_windows_$Arch.zip"
		$zip = Join-Path $tmp $file
		if ($Archive) {
			Copy-Item -LiteralPath $Archive -Destination $zip
		} else {
			$base = if ($Version -eq 'latest') {
				"https://github.com/$Repo/releases/latest/download"
			} else {
				"https://github.com/$Repo/releases/download/$Version"
			}
			note "download $file$Ellipsis"
			fetch "$base/$file" $zip
			fetch "$base/checksums.txt" (Join-Path $tmp 'checksums.txt')
			verify (Join-Path $tmp 'checksums.txt') $file
			ok 'checksum verified'
		}

		$out = Join-Path $tmp 'out'
		try {
			Expand-Archive -LiteralPath $zip -DestinationPath $out -Force
		} catch {
			die "Couldn't unpack $file" $_.Exception.Message
		}
		$new = Join-Path $out 'sptui.exe'
		if (-not (Test-Path -LiteralPath $new)) { die "There's no sptui.exe in $file" }

		$exe = Join-Path $InstallDir 'sptui.exe'
		try {
			New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
			# Windows won't replace a running sptui, but will move it aside,
			# so one that's open keeps working until it restarts.
			if (Test-Path -LiteralPath $exe) {
				Remove-Item -LiteralPath "$exe.old" -Force -ErrorAction SilentlyContinue
				Move-Item -LiteralPath $exe -Destination "$exe.old" -Force
			}
			Move-Item -LiteralPath $new -Destination $exe -Force
			Remove-Item -LiteralPath "$exe.old" -Force -ErrorAction SilentlyContinue
		} catch {
			die "Couldn't write to $InstallDir" $_.Exception.Message
		}

		# Native programs' stderr would trip ErrorActionPreference in
		# Windows PowerShell, so read it plainly.
		$ErrorActionPreference = 'Continue'
		$installed = (& $exe -version 2>&1 | Out-String).Trim()
		$code = $LASTEXITCODE
		$ErrorActionPreference = 'Stop'
		if ($code -ne 0) { die "sptui was installed but doesn't run" $installed }
		ok "$($installed -replace '^sptui ', '') in $InstallDir"
		return $exe
	}

	# add_to_path puts the install directory on the user's PATH, for new
	# terminals and for this one.
	function add_to_path([string]$Exe) {
		$key = 'HKCU:\Environment'
		# Read it unexpanded, so entries like %USERPROFILE%\bin stay as they are.
		$raw = (Get-Item -LiteralPath $key).GetValue('Path', '', 'DoNotExpandEnvironmentNames')
		$entries = @($raw -split ';' | Where-Object { $_ })
		$already = $entries | Where-Object { $_.TrimEnd('\') -eq $InstallDir.TrimEnd('\') }

		$session = @($env:Path -split ';' | Where-Object { $_ })
		if (-not ($session | Where-Object { $_.TrimEnd('\') -eq $InstallDir.TrimEnd('\') })) {
			$env:Path = "$InstallDir;$env:Path"
		}

		if (-not $already) {
			step 'PATH'
			Set-ItemProperty -LiteralPath $key -Name Path -Value (($entries + $InstallDir) -join ';') -Type ExpandString
			# Setting a variable this way tells Windows the environment
			# changed, so terminals opened from now on see the new PATH.
			[Environment]::SetEnvironmentVariable('SPTUI_INSTALLER', '1', 'User')
			[Environment]::SetEnvironmentVariable('SPTUI_INSTALLER', $null, 'User')
			ok "added $InstallDir"
		}

		$found = Get-Command sptui -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
		if ($found -and $found.Source -ne $Exe) {
			step 'PATH'
			note "Another sptui, $($found.Source), comes first on your PATH."
			note 'Remove it to use this one.'
		}
	}

	function outro {
		Write-Host
		Write-Host '  Done. Now run'
		Write-Host
		Write-Host '    sptui' -ForegroundColor Green
		Write-Host
		Write-Host '  Log in with Spotify Premium, or try  sptui -demo' -ForegroundColor DarkGray
		Write-Host '  (In a terminal that was already open, start a new one first.)' -ForegroundColor DarkGray
		Write-Host
	}

	# --- main ------------------------------------------------------------

	# Print the glyphs as themselves, not as ?s, in Windows PowerShell.
	$encoding = $null
	try {
		$encoding = [Console]::OutputEncoding
		[Console]::OutputEncoding = [Text.Encoding]::UTF8
	} catch {}
	$tmp = Join-Path ([IO.Path]::GetTempPath()) "sptui-$([guid]::NewGuid())"
	try {
		if ($env:OS -ne 'Windows_NT') {
			die 'install.ps1 is for Windows' 'On macOS and Linux:  curl -fsSL https://sptui.sh/install.sh | bash'
		}
		if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\sptui' }
		if ($Archive -and -not (Test-Path -LiteralPath $Archive -PathType Leaf)) { die "No such archive: $Archive" }
		# Windows PowerShell only offers old versions of TLS by default.
		[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
		New-Item -ItemType Directory -Path $tmp | Out-Null

		banner
		$arch = detect_arch
		$exe = install_sptui $arch
		add_to_path $exe
		outro
	} catch {
		if ($_.Exception.Message -ne 'sptui-install-failed') {
			Write-Host
			Write-Host "$Cross Install failed" -ForegroundColor Red
			Write-Host "  $($_.Exception.Message)" -ForegroundColor DarkGray
			Write-Host
		}
		# A script run from a file should fail; one piped into iex shouldn't
		# close the window it's in.
		if ($PSCommandPath) { exit 1 }
	} finally {
		Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
		if ($encoding) { try { [Console]::OutputEncoding = $encoding } catch {} }
	}
}
