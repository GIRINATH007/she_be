# SheGuard – AppWrite Setup Guide

## Prerequisites
- Docker & Docker Compose installed
- AppWrite self-hosted running

## 1. Start AppWrite (if not running)

```bash
docker run -it --rm \
  --volume /var/run/docker.sock:/var/run/docker.sock \
  --volume "$(pwd)"/appwrite:/usr/src/code/appwrite:rw \
  --entrypoint="install" \
  appwrite/appwrite:1.6.0
```

## 2. Create Project
1. Open AppWrite Console (http://localhost)
2. Create project → Name: `SheGuard`, ID: `sheguard`

## 3. Enable Email/Password Auth
1. Go to **Auth** → **Settings**
2. Enable **Email/Password** sign-in method

## 4. Create Database
- Name: `SheGuard`, ID: `sheguard`

## 5. Create Collections

### `profiles` (ID: `profiles`)
| Attribute   | Type   | Required | Size |
|------------|--------|----------|------|
| userId     | string | yes      | 36   |
| name       | string | yes      | 128  |
| phone      | string | yes      | 20   |
| bloodGroup | string | no       | 10   |
| allergies  | string | no       | 512  |
| medications| string | no       | 512  |
| pinHash    | string | no       | 255  |
| fcmToken   | string | no       | 255  |
| avatarUrl  | string | no       | 512  |

### `contacts` (ID: `contacts`)
| Attribute     | Type   | Required | Size |
|--------------|--------|----------|------|
| ownerId      | string | yes      | 36   |
| contactUserId| string | yes      | 36   |
| type         | enum   | yes      | casual, trusted |
| status       | enum   | yes      | pending, accepted, rejected |

### `locations` (ID: `locations`)
| Attribute | Type   | Required |
|----------|--------|----------|
| userId   | string | yes      |
| lat      | float  | yes      |
| lng      | float  | yes      |
| accuracy | float  | no       |
| timestamp| integer| yes      |

### `sos_events` (ID: `sos_events`)
| Attribute    | Type     | Required | Size |
|-------------|----------|----------|------|
| triggeredBy | string   | yes      | 36   |
| type        | enum     | yes      | timer, instant |
| status      | enum     | yes      | active, resolved |
| agoraChannel| string   | no       | 128  |
| startedAt   | datetime | yes      |      |
| endedAt     | datetime | no       |      |

### `walk_sessions` (ID: `walk_sessions`)
| Attribute   | Type     | Required | Size |
|------------|----------|----------|------|
| requesterId| string   | yes      | 36   |
| accepterId | string   | no       | 36   |
| status     | enum     | yes      | pending, active, completed, cancelled |
| startedAt  | datetime | yes      |      |

## 6. Create API Key
1. Go to **Settings** → **API Keys**
2. Create key with scopes: `databases.read`, `databases.write`, `users.read`
3. Copy the key → paste into `backend/.env` as `APPWRITE_API_KEY`

## 7. Add Platform
1. Go to **Settings** → **Platforms**
2. Add **Android** platform: Package name = `com.sheguard.app`
