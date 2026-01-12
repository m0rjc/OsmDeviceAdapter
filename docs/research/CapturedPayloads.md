# Captured Payloads

**Any captured payloads MUST be anonymized before committing**

## OAuth Resource Endpoint

The OAuth resource endpoint is used to retrieve the user's profile information.

https://www.onlinescoutmanager.co.uk/oauth/resource

Perform a GET request passing the access token using the `Authorization: Bearer` header:

Example payload:

```json
{"status":true,"error":null,"data":{
  "user_id":100001,
  "full_name":"John Smith",
  "email":"user@example.com",
  "profile_picture_url":null,
  "scopes":["section:member:read"],
  "sections":[{
    "section_name":"Example Scouts",
    "group_name":"1st Example Group",
    "section_id":10001,
    "group_id":1001,
    "section_type":"scouts",
    "terms":[
      {"name":"Winter 2025","startdate":"2025-09-01","enddate":"2025-12-31","term_id":50001},
      {"name":"Spring 2026","startdate":"2025-12-31","enddate":"2026-04-12","term_id":50002}],
    "upgrades":{"level":"gold","badges":true,"campsiteexternalbookings":false,"details":true,
      "events":true,"emailbolton":true,"programme":true,"accounts":false,
      "filestorage":false,"chat":false,"ai":false,"tasks":false,"at_home":true}
  }],
  "has_parent_access":false,
  "has_section_access":true},
  "meta":[]
}
```

Things to note:

* The `sections` array contains the user's sections and their terms.
* Terms are returned in chronological order. All requests we make should use the current term.

## Patrol Information Endpoint

This is a GET request to

https://www.onlinescoutmanager.co.uk/ext/members/patrols/?action=getPatrolsWithPeople&sectionid=10001&termid=50002&include_no_patrol=y

### Response Structure

The response is a JSON object where each key is either:
- A patrol ID (can be negative for special patrols, or positive for regular patrols)
- The string "unallocated" for members not assigned to any patrol

### Identifying Special Patrols

Special/administrative patrols are identified by:
1. **Negative patrol IDs** (e.g., "-3" for Young Leaders, "-2" for Leaders)
2. Empty `members` arrays
3. Typically have `points: "0"`

Regular patrols have positive numeric IDs and contain member arrays.

### Reading Points

Points are stored in the `points` field as a string value (e.g., "47", "30", "32").

### Patrol Types Found

**Regular Patrols** (displayed in UI):
- Eagles - 32 points
- Lions - 30 points
- Wolves - 47 points
- Unallocated Members (special key for members without patrol assignment)

**Special/Administrative Patrols**:
- Young Leaders (YLs) - patrol ID: "-3"
- Leaders - patrol ID: "-2"
- Snr Patrol Leader - This will be a special patrol when we have a senior patrol leader in our troop. It is not needed for this work.

### Member Fields

Each member object contains:
- `firstname`, `lastname` - member name
- `scout_id`, `scoutid` - member identifiers
- `photo_guid` - photo reference (can be null)
- `patrolid` - patrol assignment (0 for unallocated)
- `patrolleader` - leadership role: "2" = PL, "1" = APL, "0" = member
- `patrol_role_level`, `patrol_role_level_label`, `patrol_role_level_abbr` - role information
- `age` - formatted as "years / months"
- `sectionid` - section identifier
- `active` - membership status
- `enddate` - membership end date (typically null for active members)

### Example Payload

```json
{
  "-3": {
    "patrolid": "-3",
    "sectionid": "-2",
    "name": "Young Leaders (YLs)",
    "active": "1",
    "points": "0",
    "census_costs": false,
    "members": []
  },
  "-2": {
    "patrolid": "-2",
    "sectionid": "-2",
    "name": "Leaders",
    "active": "1",
    "points": "0",
    "census_costs": false,
    "members": []
  },
  "72699": {
    "patrolid": "72699",
    "sectionid": "10001",
    "name": "Wolves",
    "active": "1",
    "points": "47",
    "census_costs": false,
    "members": [
      {
        "firstname": "John",
        "lastname": "Smith",
        "scout_id": 1000001,
        "photo_guid": "00000000-0000-0000-0000-000000000001",
        "patrolid": 72699,
        "patrolleader": "2",
        "patrol": "Wolves PL",
        "sectionid": 10001,
        "enddate": null,
        "age": "13 / 6",
        "patrol_role_level_label": "Patrol Leader",
        "active": 1,
        "scoutid": 1000001,
        "patrol_role_level_abbr": "PL",
        "patrol_role_level": 2,
        "editable": true
      }
    ]
  },
  "72700": {
    "patrolid": "72700",
    "sectionid": "10001",
    "name": "Lions",
    "active": "1",
    "points": "30",
    "census_costs": false,
    "members": [
      {
        "firstname": "Jane",
        "lastname": "Doe",
        "scout_id": 1000002,
        "photo_guid": "00000000-0000-0000-0000-000000000002",
        "patrolid": 72700,
        "patrolleader": "1",
        "patrol": "Lions APL",
        "sectionid": 10001,
        "enddate": null,
        "age": "12 / 8",
        "patrol_role_level_label": "Assistant Patrol Leader",
        "active": 1,
        "scoutid": 1000002,
        "patrol_role_level_abbr": "APL",
        "patrol_role_level": 1,
        "editable": true
      }
    ]
  },
  "132322": {
    "patrolid": "132322",
    "sectionid": "10001",
    "name": "Eagles",
    "active": "1",
    "points": "32",
    "census_costs": false,
    "members": [
      {
        "firstname": "Bob",
        "lastname": "Johnson",
        "scout_id": 1000003,
        "photo_guid": "00000000-0000-0000-0000-000000000003",
        "patrolid": 132322,
        "patrolleader": "0",
        "patrol": "Eagles",
        "sectionid": 10001,
        "enddate": null,
        "age": "11 / 10",
        "patrol_role_level_label": "",
        "active": 1,
        "scoutid": 1000003,
        "editable": true
      }
    ]
  },
  "175221": {
    "patrolid": "175221",
    "sectionid": "10001",
    "name": "Snr Patrol Leader",
    "active": "1",
    "points": "0",
    "census_costs": false,
    "members": []
  },
  "unallocated": {
    "members": [
      {
        "firstname": "Alice",
        "lastname": "Williams",
        "scout_id": 1000004,
        "photo_guid": "00000000-0000-0000-0000-000000000004",
        "patrolid": 0,
        "patrolleader": "0",
        "patrol": "",
        "sectionid": 10001,
        "enddate": null,
        "age": "10 / 9",
        "patrol_role_level_label": "",
        "active": 1,
        "scoutid": 1000004,
        "editable": true
      }
    ]
  }
}
```
