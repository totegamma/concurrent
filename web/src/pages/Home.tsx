import { Box, Button, Fade, Tab, Tabs } from "@mui/material"
import {  useState } from "react"
import { Navigate, useLocation } from "react-router-dom"
import { Entity } from "../model"
import { Entities } from "../widgets/entities"
import { Hosts } from "../widgets/hosts"

export const Home = (): JSX.Element => {

    const [tab, setTab] = useState(0)
    const entityJson = localStorage.getItem("ENTITY")
    const entity = entityJson ? (JSON.parse(entityJson) as Entity) : null

    if (!entity) return <Navigate to='/welcome' state={{ from: useLocation() }} replace={true} />
    return (
        <>
            hello {entity?.ccaddr}<br />
            your role is {entity?.role}

            <Tabs
                value={tab}
                onChange={(_, index) => {
                    setTab(index)
                }}
            >
                <Tab label='Hello' />
                {entity?.role === '_admin' && <Tab label="Entities" />}
                {entity?.role === '_admin' && <Tab label="Hosts" />}
            </Tabs>

            <Box sx={{ position: 'relative', mt: '20px' }}>
                <Fade in={tab === 0} unmountOnExit>
                    <Box sx={{
                        position: 'absolute',
                        width: '100%',
                        display: 'flex',
                        flexDirection: 'column',
                        gap: 1
                    }}>
                        まだ未実装の機能たち
                        <Button variant='contained'>招待コードの発行</Button>
                        <Button variant='contained'>アカウントの転出</Button>
                        <Button color='error' variant='contained'>アカウントの凍結</Button>
                    </Box>
                </Fade>
                <Fade in={tab === 1} unmountOnExit>
                    <Entities />
                </Fade>
                <Fade in={tab === 2} unmountOnExit>
                    <Hosts />
                </Fade>
            </Box>
        </>
    )
}
