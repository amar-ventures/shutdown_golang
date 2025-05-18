Here’s the updated README.md with all the recent changes incorporated:

```markdown
# Shutdown Daemon

This project is a Go-based daemon that interacts with a Supabase database to manage device statuses and handle shutdown requests. It uses Supabase's REST API for authentication and database operations.

## Features

- **Authentication**: Authenticates users using Supabase's email-password authentication.
- **Device Status Management**: Periodically updates the device's status (`on`, `off`, or `unknown`) in the `devices` table.
- **Shutdown Request Handling**: Listens for shutdown requests and executes a system shutdown if a valid request is received.
- **Row-Level Security (RLS)**: Adheres to Supabase's RLS policies for secure database access.
- **Cross-Platform Support**: Supports both Linux and Windows with platform-specific binaries and startup scripts.

## Prerequisites

- Go 1.20 or later installed on your system.
- A Supabase project with the following table and policies:

### Supabase Table Schema

```sql
create table devices (
  id uuid default uuid_generate_v4() primary key,
  user_id uuid references auth.users not null,
  name text not null,
  status text check (status in ('on', 'off', 'unknown')) not null default 'unknown',
  last_seen timestamp with time zone,
  first_online_at timestamp with time zone,
  shutdown_requested jsonb,
  created_at timestamp with time zone default timezone('utc'::text, now()) not null
);

-- Enable RLS
alter table devices enable row level security;

-- Select policy
create policy "Users can view their own devices"
  on devices for select
  using (auth.uid() = user_id);

-- Update policy
create policy "Users can update their own devices"
  on devices for update
  using (auth.uid() = user_id);

-- Insert policy
create policy "Users can create their own devices"
  on devices for insert
  with check (auth.uid() = user_id);
```

## Installation

### Linux

1. Build the Linux binary:
   ```bash
   GOOS=linux GOARCH=amd64 go build -o shutdown_daemon_linux app.go
   ```

2. Use the provided `install_shutdown_daemon.sh` script to install the binary, `.env` file, and create a systemd service:
   ```bash
   chmod +x install_shutdown_daemon.sh
   ./install_shutdown_daemon.sh
   ```

3. The script will:
   - Copy the binary and `.env` file to `~/basescripts/shutdown_golang`.
   - Create a systemd service named `shutdown_golang.service`.
   - Enable and start the service.

4. Verify the service:
   ```bash
   sudo systemctl status shutdown_golang.service
   ```

### Windows

1. Build the Windows binary:
   ```bash
   GOOS=windows GOARCH=amd64 go build -o shutdown_daemon_windows.exe app.go
   ```

2. Use the provided `install_shutdown_daemon.bat` script to set up the binary as an autostart program:
   - Save the script as `install_shutdown_daemon.bat` in the same directory as the binary and `.env` file.
   - Run the script as an administrator:
     ```bat
     install_shutdown_daemon.bat
     ```

3. The script will:
   - Copy the binary and `.env` file to `%USERPROFILE%\basescripts\shutdown_golang`.
   - Create a Task Scheduler task named `ShutdownGolangDaemon` to run the binary on user login.

4. Verify the task:
   - Open Task Scheduler (`Win + R`, type `taskschd.msc`, and press Enter).
   - Look for the task named `ShutdownGolangDaemon` and test it by right-clicking and selecting **Run**.

## Usage

1. The daemon will:
   - Authenticate the user using Supabase's REST API.
   - Ensure a row exists for the current device (using the hostname as the device name).
   - Periodically update the device's status in the database.
   - Listen for shutdown requests and execute a system shutdown if a valid request is received.

2. Logs:
   - The daemon logs its activity to the console, including:
     - Authentication success or failure.
     - Device status updates.
     - Shutdown request handling.
     - Errors encountered during database operations.

## Troubleshooting

- **403 Errors**: Ensure the correct RLS policies are in place for the `devices` table.
- **Authentication Issues**: Verify the `SUPABASE_URL`, `SUPABASE_KEY`, `USER_EMAIL`, and `USER_PASSWORD` in your `.env` file.
- **Device Not Found**: The daemon will automatically create a device row if it doesn't exist. Ensure the `Insert` policy is correctly configured.

## Development

### Building Cross-Platform Binaries

- **Linux**:
  ```bash
  GOOS=linux GOARCH=amd64 go build -o shutdown_daemon_linux app.go
  ```

- **Windows**:
  ```bash
  GOOS=windows GOARCH=amd64 go build -o shutdown_daemon_windows.exe app.go
  ```

### Testing Locally

- Run the binary directly:
  ```bash
  ./shutdown_daemon_linux
  ```

- For Windows:
  ```cmd
  shutdown_daemon_windows.exe
  ```

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
```

This updated README.md includes:
1. Instructions for both Linux and Windows installation.
2. Details about the systemd service and Task Scheduler setup.
3. Cross-platform build commands.
4. Troubleshooting tips for common issues.

Let me know if you need further adjustments!