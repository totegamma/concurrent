import { Box, Button, Fade, Tab, Tabs, TextField, Typography } from "@mui/material"
import { useEffect, useState } from "react"
import { Navigate, useLocation } from "react-router-dom"
import { Entity, Host } from "../model"
import { getHosts, getEntities, sayHello } from '../util'

export const Home = (): JSX.Element => {

    const [tab, setTab] = useState(0)
    const entityJson = localStorage.getItem("ENTITY")
    const token = localStorage.getItem("JWT")
    const entity = entityJson ? (JSON.parse(entityJson) as Entity) : null

    const [entities, setEntities] = useState<Entity[]>([])
    const [hosts, setHosts] = useState<Host[]>([])

    const [remoteFqdn, setRemoteFqdn] = useState('')

    useEffect(() => {
        getHosts().then(setHosts)
        getEntities().then(setEntities)
    }, [])

    if (!entity) return <Navigate to='/welcome' state={{ from: useLocation() }} replace={true} />
    return (
        <>
            hello {entity?.ccaddr}<br />
            your role is {entity?.role}

            {entity?.role === '_admin' && (<>
            <Tabs
                value={tab}
                onChange={(_, index) => {
                    setTab(index)
                }}
            >
                <Tab label="Entities" />
                <Tab label="Hosts" />
            </Tabs>

            <Box sx={{ position: 'relative', mt: '20px' }}>
                <Fade in={tab === 0} unmountOnExit>
                    <Box sx={{ position: 'absolute', width: '100%' }}>
                        <Typography>Entities</Typography>
                        <pre>{JSON.stringify(entities, null, 2)}</pre>
                    </Box>
                </Fade>
                <Fade in={tab === 1} unmountOnExit>
                    <Box sx={{ position: 'absolute', width: '100%' }}>
                        <Box sx={{ display: 'flex', gap: '10px' }}>
                            <TextField
                                label="remote fqdn"
                                variant="outlined"
                                value={remoteFqdn}
                                sx={{ flexGrow: 1 }}
                                onChange={(e) => {
                                    setRemoteFqdn(e.target.value)
                                }}
                            />
                            <Button
                                variant="contained"
                                onClick={(_) => {
                                    if (!token) return
                                    sayHello(token, remoteFqdn)
                                }}
                            >
                                GO
                            </Button>
                        </Box>
                        <Typography>Hosts</Typography>
                        <pre>{JSON.stringify(hosts, null, 2)}</pre>
                    </Box>
                </Fade>
            </Box>
            </>)}
        </>
    )

}
