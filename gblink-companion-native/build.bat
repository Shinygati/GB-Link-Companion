@echo off
rem Builds GB-Link Companion under WSL Ubuntu:
rem   build.bat          Windows .exe and Linux AppImage
rem   build.bat windows  only the Windows .exe
rem   build.bat linux    only the Linux AppImage
rem Results go in the dist folder next to this file. Needs build.sh beside it.

setlocal
set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=all"

where wsl >nul 2>nul
if errorlevel 1 (
  echo WSL isn't installed. Install it with: wsl --install -d Ubuntu
  goto :end
)

if not exist "%~dp0build.sh" (
  echo build.sh is missing: put it in the same folder as build.bat.
  goto :end
)

rem tr strips Windows line endings, in case Git checked build.sh out with CRLF
wsl --cd "%~dp0" bash -c "tr -d '\r' < build.sh | bash -s -- %TARGET%"
if errorlevel 1 (
  echo.
  echo Build failed - see the messages above.
) else (
  echo.
  echo Done. Files are in "%~dp0dist"
)

:end
rem keep the window open when double-clicked
rem (Windows' own findstr by full path: a Unix "find" from Git or MSYS can come first on PATH)
echo %cmdcmdline% | "%SystemRoot%\System32\findstr.exe" /i /c:" /c " >nul && pause
endlocal