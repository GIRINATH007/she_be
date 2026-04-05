# Supabase Migration Note

This project no longer uses Appwrite.

The backend and frontend now expect Supabase for:
- Authentication
- Profiles
- Contacts
- SOS events
- Walk sessions

Use these backend env vars:
- SUPABASE_URL
- SUPABASE_ANON_KEY
- SUPABASE_SERVICE_ROLE_KEY

Use these frontend env vars:
- EXPO_PUBLIC_SUPABASE_URL
- EXPO_PUBLIC_SUPABASE_ANON_KEY

Detailed Supabase setup steps will be provided in the next prompt as requested.