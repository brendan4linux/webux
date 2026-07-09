In internal/system/containers/interface.go, add RunContainer to the Runtime interface.

Find the interface block (around line 85) and add:

    RunContainer(ctx context.Context, cfg RunConfig) (string, error)

The full interface should include it alongside Start, Stop, Restart, Remove etc.

---

In internal/system/containers/docker.go, check if postBody exists.
If not (only `post` exists), add this method:

    func (d *Docker) postBody(ctx context.Context, path string, body interface{}, out interface{}) error {
        b, err := json.Marshal(body)
        if err != nil {
            return err
        }
        resp, err := d.client.Post("http://docker"+path, "application/json", bytes.NewReader(b))
        if err != nil {
            return err
        }
        defer resp.Body.Close()
        if resp.StatusCode >= 300 {
            buf, _ := io.ReadAll(resp.Body)
            return fmt.Errorf("docker API %s: %s", path, strings.TrimSpace(string(buf)))
        }
        if out != nil {
            return json.NewDecoder(resp.Body).Decode(out)
        }
        return nil
    }

Do the same for podman.go replacing `d.client` with `p.client` and "docker" with "podman".
