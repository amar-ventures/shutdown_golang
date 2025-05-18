@echo off

:: Variables
set INSTALL_DIR=%USERPROFILE%\basescripts\shutdown_golang
set BINARY_NAME=shutdown_daemon_windows.exe
set ENV_FILE=.env
set TASK_NAME=ShutdownGolangDaemon

:: Create installation directory
echo Creating installation directory at %INSTALL_DIR%...
mkdir "%INSTALL_DIR%"

:: Copy binary and .env file
echo Copying binary and .env file to %INSTALL_DIR%...
copy "%BINARY_NAME%" "%INSTALL_DIR%\"
copy "%ENV_FILE%" "%INSTALL_DIR%\"

:: Create a Task Scheduler task to run the binary on startup
echo Creating Task Scheduler task...
schtasks /create /tn "%TASK_NAME%" /tr "%INSTALL_DIR%\%BINARY_NAME%" /sc onlogon /rl highest /f

:: Confirm task creation
echo Task Scheduler task "%TASK_NAME%" created successfully.