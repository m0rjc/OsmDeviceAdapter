# Note on API Obfuscation

This system runs on a small server. It uses rate limiting to reduce impact
of abuse, but the effect may be limited. We live in a world where robots scan webservers
looking for weaknesses. At some time I may need to obfuscate my endpoints to make them
harder for these bots to find.

If endpoints are obfuscated then your client will need a
means to configure the `/oauth`, `/device` and `/api` paths. Whether I document the new
paths will be risk-assessed at the time. I expect the robots are not clever enough to connect
my server to this codebase. If I come under a targeted attack then I am in bigger trouble.