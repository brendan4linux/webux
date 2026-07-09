In internal/api/handlers/databases.go (or wherever MySQL connection is built),
find where the DSN is constructed. It likely looks like:

    dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", user, password, host, port, dbname)

The issue: when password is empty string, MySQL driver still sends authentication
with an empty password — but if the server expects no password at all (root with
no password), the DSN must use an empty string correctly.

The real bug is likely in how the frontend sends the password. In the database
connection modal, if the password field is left blank, the JSON body should send
`"password": ""` not omit the field. Check the handler:

    var body struct {
        Host     string `json:"host"`
        Port     int    `json:"port"`
        User     string `json:"user"`
        Password string `json:"password"`   // ← must be `string` not `*string`
        DBName   string `json:"db_name"`
    }

If Password is `*string` (pointer), an empty field in JSON unmarshals as nil,
and nil gets formatted as "<nil>" in the DSN string — which causes auth failure.

Fix: ensure Password field is plain `string` (not pointer), so empty JSON string
unmarshals as "" and the DSN becomes "root:@tcp(...)/" which MySQL accepts.

Also check the DSN construction handles empty password:
    if body.Password == "" {
        dsn = fmt.Sprintf("%s:@tcp(%s:%d)/%s", body.User, body.Host, body.Port, body.DBName)
    } else {
        dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", body.User, body.Password, body.Host, body.Port, body.DBName)
    }
Both are equivalent for the MySQL driver but making it explicit avoids confusion.
